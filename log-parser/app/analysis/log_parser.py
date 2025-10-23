import os
import re
import json
import logging
from enum import Enum, auto
from datetime import datetime
from zoneinfo import ZoneInfo
from dataclasses import dataclass
from typing import Final, Optional
from pydantic import ValidationError
from urllib.parse import urlparse, parse_qs, unquote

from app.model.mmp_savedata import MmpSaveData, MmpSaveRecord


class Event(Enum):
    DEKAPU_SAVEDATA_UPDATE = auto()
    DEKAPU_WORLD_JOIN = auto()
    DEKAPU_WORLD_LEAVE = auto()
    DEKAPU_SESSION_RESET = auto()
    DEKAPU_CLOUD_LOAD = auto()
    DEKAPU_JP_STOCKOVER = auto()
    VRCHAT_APP_QUIT = auto()
    TIMESTAMP_UPDATE = auto()


@dataclass
class ParseResult:
    event: Event  # イベント種別

    record: Optional[MmpSaveRecord] = None  # MMPセーブデータレコード
    stockover_value: Optional[int] = None  # JPストック溢れ値用
    new_timestamp: Optional[datetime] = None  # タイムスタンプ更新用


class MppLogParser:
    SAVEDATA_URL_PATTERN: Final[re.Pattern] = re.compile(
        r"https://push\.trap\.games/api/v\d+/data"
    )
    CLOUD_LOAD_MSG: Final[str] = "[LoadFromParsedData]"
    SESSION_RESET_MSG: Final[str] = "[ResetCurrentSession]"
    JP_STOCK_OVER_MSG: Final[str] = "[JP] ストック溢れです"
    WORLD_JOIN_MSG: Final[str] = (
        "[Behaviour] Joining wrld_1af53798-92a3-4c3f-99ae-a7c42ec6084d"
    )
    WORLD_LEAVE_MSG: Final[str] = "[OnPlayerLeft] ローカルプレイヤーが Leave した"
    VRCHAT_APP_QUIT_MSG: Final[str] = "VRCApplication: HandleApplicationQuit"

    DSM_TIMESTAMP_PREFIX: Final[str] = "[DSM SaveURL] Generated URL"
    TIMESTAMP_RE = re.compile(r"^(\d{4}\.\d{2}\.\d{2} \d{2}:\d{2}:\d{2})")

    DEFAULT_TZ: Final[str] = "Asia/Tokyo"

    def __init__(self, fname: str):
        self.fname = fname

        # TZ環境変数がない場合はAsia/Tokyoとして解釈する
        tz_name = os.getenv("TZ", self.DEFAULT_TZ)
        try:
            self.tz = ZoneInfo(tz_name)
        except Exception:
            # タイムゾーンが不正の場合はAsia/Tokyoとして解釈する
            logging.warning(f"[{self.fname}] Invalid timezone. ({tz_name})")
            self.tz = ZoneInfo(self.DEFAULT_TZ)

    def _parse_timestamp_line(self, line: str) -> Optional[datetime]:
        # YYYY.MM.DD HH:MM:SS形式のタイムスタンプを抽出
        m = self.TIMESTAMP_RE.match(line)
        if not m:
            return

        try:
            ts = datetime.strptime(m.group(1), "%Y.%m.%d %H:%M:%S")
            # InfluxDBで扱うためUTCに変換
            return ts.replace(tzinfo=self.tz).astimezone(ZoneInfo("UTC"))
        except Exception as e:
            logging.warning(f"[{self.fname}] Failed to parse timestamp: {e}")

    def _parse_jp_stockover_line(self, line: str) -> Optional[int]:
        m = re.search(r":\s*([\d,]+)$", line)
        if not m:
            return

        try:
            return int(m.group(1).replace(",", ""))
        except Exception as e:
            logging.warning(f"[{self.fname}] Failed to parse JP stockover: {e}")

    def parse_line(self, line: str) -> Optional[ParseResult]:
        try:
            # セーブデータのタイムスタンプ行の検出
            if self.DSM_TIMESTAMP_PREFIX in line:
                ts = self._parse_timestamp_line(line)
                return ParseResult(event=Event.TIMESTAMP_UPDATE, new_timestamp=ts)

            # クラウドロードの検出
            if self.CLOUD_LOAD_MSG in line:
                return ParseResult(event=Event.DEKAPU_CLOUD_LOAD)

            # セッションリセットの検出
            if self.SESSION_RESET_MSG in line:
                return ParseResult(event=Event.DEKAPU_SESSION_RESET)

            # JPストック溢れの検出
            if self.JP_STOCK_OVER_MSG in line:
                value = self._parse_jp_stockover_line(line)
                return ParseResult(
                    event=Event.DEKAPU_JP_STOCKOVER, stockover_value=value
                )

            # でかプへのJoin検出
            if self.WORLD_JOIN_MSG in line:
                return ParseResult(event=Event.DEKAPU_WORLD_JOIN)

            # でかプからLeave検出
            if self.WORLD_LEAVE_MSG in line:
                return ParseResult(event=Event.DEKAPU_WORLD_LEAVE)

            # VRChatアプリ終了検出
            if self.VRCHAT_APP_QUIT_MSG in line:
                return ParseResult(event=Event.VRCHAT_APP_QUIT)

            # セーブデータ行の解析
            if self.SAVEDATA_URL_PATTERN.search(line):
                parsed = urlparse(line)
                query = parse_qs(parsed.query)

                raw_data = self._get_query_param(query, "data")
                if not raw_data:
                    return None

                user_id = self._get_query_param(query, "user_id")
                if not user_id:
                    return None

                sig = self._get_query_param(query, "sig")
                if not sig:
                    return None

                try:
                    raw_dict: dict[str, any] = json.loads(raw_data)
                    data = MmpSaveData(**raw_dict)
                except ValidationError as e:
                    logging.warning(f"[{self.fname}] Save data validation error: {e}")
                    return None
                except json.JSONDecodeError as e:
                    logging.warning(f"[{self.fname}] JSON decode error: {e}")
                    return None

                return ParseResult(
                    event=Event.DEKAPU_SAVEDATA_UPDATE,
                    record=MmpSaveRecord(
                        data=data,
                        user_id=user_id,
                        sig=sig,
                        raw_url=line,
                    ),
                )

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

    def _get_query_param(self, query: dict[str, list[str]], key: str) -> Optional[str]:
        values = query.get(key)
        if not values or not values[0].strip():
            logging.warning(f"[{self.fname}] Missing or empty parameter: {key}")
            return None
        return unquote(values[0])

    def fix_overflow(self, value: int, bits: int = 32):
        if value < 0:
            mask = (1 << bits) - 1
            return value & mask
        return value
