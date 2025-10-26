import re
import json
import logging
from enum import Enum, auto
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


@dataclass
class ParseResult:
    event: Event  # イベント種別
    record: Optional[MmpSaveRecord] = None  # MMPセーブデータレコード
    stockover_value: Optional[int] = None  # JPストック溢れ値用


class MmpLogParser:
    DEKAPU_WORLD_ID: Final[str] = "wrld_1af53798-92a3-4c3f-99ae-a7c42ec6084d"

    SAVEDATA_URL_PATTERN = re.compile(r"https://push\.trap\.games/api/v\d+/data")

    CLOUD_LOAD_MSG: Final[str] = "[LoadFromParsedData]"
    SESSION_RESET_MSG: Final[str] = "[ResetCurrentSession]"
    JP_STOCK_OVER_MSG: Final[str] = "[JP] ストック溢れです"
    WORLD_JOIN_MSG: Final[str] = f"[Behaviour] Joining {DEKAPU_WORLD_ID}"
    WORLD_LEAVE_MSG: Final[str] = "[OnPlayerLeft] ローカルプレイヤーが Leave した"
    VRCHAT_APP_QUIT_MSG: Final[str] = "VRCApplication: HandleApplicationQuit"

    def __init__(self, fname: str):
        self.fname = fname

    def _parse_jp_stockover_line(self, line: str) -> Optional[int]:
        m = re.search(r":\s*([\d,]+)$", line)
        if not m:
            return None

        raw = m.group(1)
        try:
            return int(raw.replace(",", ""))
        except ValueError as e:
            logging.warning(f"[{self.fname}] Failed to parse JP stockover '{raw}': {e}")
            return None

    def parse_line(self, line: str) -> Optional[ParseResult]:
        try:
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
