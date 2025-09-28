import os
import re
import json
import logging
from datetime import datetime
from zoneinfo import ZoneInfo
from typing import Final, Optional
from influxdb_client import Point, WritePrecision
from urllib.parse import urlparse, parse_qs, unquote

from app.analysis.credit_speed import CreditSpeed


class MppLogParser:
    SAVEDATA_URL_PREFIX: Final[str] = "https://push.trap.games/api/v3/data"
    TIMESTAMP_PREFIX: Final[str] = "[DSM SaveURL] Generated URL"

    TIMESTAMP_RE = re.compile(r"^(\d{4}\.\d{2}\.\d{2} \d{2}:\d{2}:\d{2})")

    DEFAULT_TZ: Final[str] = "Asia/Tokyo"

    def __init__(self, fname: str):
        self.fname = fname
        self.credit_calc = CreditSpeed()
        self.last_timestamp: Optional[datetime] = None

        # TZ環境変数がない場合はAsia/Tokyoとして解釈する
        tz_name = os.getenv("TZ", self.DEFAULT_TZ)
        try:
            self.tz = ZoneInfo(tz_name)
        except Exception:
            # タイムゾーンが不正の場合はAsia/Tokyoとして解釈する
            logging.warning(
                f"[{self.fname}] Invalid timezone. ({tz_name})"
            )
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

    def parse_line(self, line: str) -> Point | None:
        # タイムスタンプ行の検出
        if self.TIMESTAMP_PREFIX in line:
            self._parse_timestamp_line(line)
            return None

        # セーブデータ行の検出
        if self.SAVEDATA_URL_PREFIX not in line:
            return None

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

        # タイムスタンプが未取得の場合、現在時刻で書き込む
        timestamp = self.last_timestamp or datetime.now(tz=ZoneInfo("UTC"))
        if not self.last_timestamp:
            logging.warning(f"[{self.fname}] No timestamp captured, fallback to now()")

        p = (
            Point("mpp-savedata")
            .tag("user", user_id)
            .time(timestamp, WritePrecision.NS)
        )

        for k, v in data.items():
            if isinstance(v, (int, float, str)):
                p = p.field(k, v)
            elif isinstance(v, dict) and k.startswith("dc_"):
                # dc_から始まるものは子要素のキー名をアンダースコアで結合して追加
                for sub_k, sub_v in v.items():
                    if isinstance(sub_v, (int, float, str)):
                        p = p.field(f"{k}_{sub_k}", sub_v)

        if credit_all is not None:
            delta = self.credit_calc.add(credit_all, timestamp)
            if delta is not None:
                p = p.field("credit_all_delta_1m", delta)

        return p
