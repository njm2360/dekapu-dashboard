import os
import re
import json
import logging
from datetime import datetime
from zoneinfo import ZoneInfo
from typing import Final, Optional
from influxdb_client import Point, WritePrecision
from urllib.parse import urlparse, parse_qs, unquote

from app.analysis.medal_rate_ema import MedalRateEMA


class MppLogParser:
    SAVEDATA_URL_PREFIX: Final[str] = "https://push.trap.games/api/v3/data"
    TIMESTAMP_PREFIX: Final[str] = "[DSM SaveURL] Generated URL"
    CLOUD_LOAD_MSG: Final[str] = "[LoadFromParsedData]"
    SESSION_RESET_MSG: Final[str] = "[ResetCurrentSession]"
    JP_STOCK_OVER_MSG: Final[str] = "[JP] ストック溢れです"
    WORLD_JOIN_MSG: Final[str] = (
        "[Behaviour] Joining wrld_1af53798-92a3-4c3f-99ae-a7c42ec6084d"
    )

    TIMESTAMP_RE = re.compile(r"^(\d{4}\.\d{2}\.\d{2} \d{2}:\d{2}:\d{2})")

    DEFAULT_TZ: Final[str] = "Asia/Tokyo"

    def __init__(self, fname: str):
        self.fname = fname
        self.medal_rate = MedalRateEMA()
        self.last_timestamp: Optional[datetime] = None
        self.last_stockover: int = 0

        # TZ環境変数がない場合はAsia/Tokyoとして解釈する
        tz_name = os.getenv("TZ", self.DEFAULT_TZ)
        try:
            self.tz = ZoneInfo(tz_name)
        except Exception:
            # タイムゾーンが不正の場合はAsia/Tokyoとして解釈する
            logging.warning(f"[{self.fname}] Invalid timezone. ({tz_name})")
            self.tz = ZoneInfo(self.DEFAULT_TZ)

    def _parse_timestamp_line(self, line: str):
        # YYYY.MM.DD HH:MM:SS形式のタイムスタンプを抽出
        m = self.TIMESTAMP_RE.match(line)
        if not m:
            return

        try:
            ts = datetime.strptime(m.group(1), "%Y.%m.%d %H:%M:%S")
            # InfluxDBで扱うためUTCに変換
            self.last_timestamp = ts.replace(tzinfo=self.tz).astimezone(ZoneInfo("UTC"))
        except Exception as e:
            logging.warning(f"[{self.fname}] Failed to parse timestamp: {e}")

    def _parse_jp_stockover_line(self, line: str):
        m = re.search(r":\s*([\d,]+)$", line)
        if not m:
            return

        try:
            num_str = m.group(1)
            stock = int(num_str.replace(",", ""))
            self.last_stockover += stock
            logging.debug(f"[{self.fname}] JP stockover added: {stock}")
        except Exception as e:
            logging.warning(f"[{self.fname}] Failed to parse JP stockover: {e}")

    def parse_line(self, line: str) -> Optional[Point]:
        try:
            # タイムスタンプ行の検出
            if self.TIMESTAMP_PREFIX in line:
                self._parse_timestamp_line(line)
                return None

            # クラウドロードの検出
            if self.CLOUD_LOAD_MSG in line:
                logging.info(f"[{self.fname}] Cloud load detected. Reset medal rate.")
                self.medal_rate.reset()
                return None

            # セッションリセットの検出(パーク振り直し)
            if self.SESSION_RESET_MSG in line:
                logging.info(
                    f"[{self.fname}] Session reset detected. Reset medal rate."
                )
                self.medal_rate.reset()
                return None

            # JPストック溢れの検出
            if self.JP_STOCK_OVER_MSG in line:
                self._parse_jp_stockover_line(line)

            # でかプへのJoin検出
            if self.WORLD_JOIN_MSG in line:
                logging.info(
                    f"[{self.fname}] Dekapu world join detected. Reset medal rate."
                )
                self.medal_rate.reset()
                return None

            # セーブデータ行の検出
            if self.SAVEDATA_URL_PREFIX in line:
                parsed = urlparse(line)
                query = parse_qs(parsed.query)
                raw_data = unquote(query.get("data", ["{}"])[0])

                try:
                    data: dict[str, any] = json.loads(raw_data)
                except json.JSONDecodeError as e:
                    logging.warning(f"[{self.fname}] JSON decode error: {e}")
                    return None

                user_id = query.get("user_id", [""])[0]
                credit_all = data.get("credit_all")
                l_achieve_count = len(data.get("l_achieve", []))

                # タイムスタンプが未取得の場合、現在時刻で書き込む
                timestamp = self.last_timestamp or datetime.now(tz=ZoneInfo("UTC"))
                if not self.last_timestamp:
                    logging.warning(
                        f"[{self.fname}] No timestamp captured, fallback to now()"
                    )

                p = (
                    Point("mpp-savedata")
                    .tag("user", user_id)
                    .time(timestamp, WritePrecision.NS)
                    .field("l_achieve_count", l_achieve_count)
                )

                for k, v in data.items():
                    if isinstance(v, (int, float, str)):
                        if isinstance(v, int):
                            v = self.fix_overflow(v, 32)
                        p = p.field(k, v)
                    elif isinstance(v, dict) and k.startswith("dc_"):
                        for sub_k, sub_v in v.items():
                            if isinstance(sub_v, (int, float, str)):
                                if isinstance(sub_v, int):
                                    sub_v = self.fix_overflow(sub_v, 32)
                                p = p.field(f"{k}_{sub_k}", sub_v)
                    elif isinstance(v, list) and k.startswith("l_totems_set"):
                        for i, item in enumerate(v, start=1):
                            if isinstance(item, int):
                                p = p.field(f"{k}_{i}", item)

                if credit_all is not None:
                    # ストック溢れ分を差し引いて増加量を計算
                    adjusted_credit = credit_all - self.last_stockover
                    delta = self.medal_rate.update(adjusted_credit, timestamp)
                    if delta is not None:
                        p = p.field("credit_all_delta_1m", delta)
                        logging.debug(f"[{self.fname}] Credit delta: {delta}/min")

                return p

            return None

        except UnicodeDecodeError as e:
            logging.error(f"[{self.fname}] Encoding error in line: {e}")
            return None
        except (TypeError, KeyError) as e:
            logging.error(f"[{self.fname}] Unexpected data format: {e}")
            return None
        except Exception as e:
            logging.exception(f"[{self.fname}] Unexpected parser error: {e}")
            return None

    def fix_overflow(self, value: int, bits: int = 32):
        if value < 0:
            mask = (1 << bits) - 1
            return value & mask
        return value
