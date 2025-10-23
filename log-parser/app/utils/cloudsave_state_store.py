import json
import asyncio
import logging
from pathlib import Path
from datetime import datetime
from typing import Dict, Final, Optional

from pydantic import BaseModel, ConfigDict, Field


class CloudSaveState(BaseModel):
    credit_all: int = Field(..., alias="creditAll")
    last_attempt_at: datetime = Field(..., alias="lastAttemptAt")
    data_hash: str = Field(..., alias="dataHash")

    model_config = ConfigDict(populate_by_name=True)


class CloudSaveStateStore:
    VERSION: Final[int] = 2

    def __init__(self, path: Path):
        self._path = path
        self._lock = asyncio.Lock()
        self._data: Dict[str, CloudSaveState] = {}

        if self._path.exists():
            try:
                with open(self._path, "r", encoding="utf-8") as f:
                    raw: dict = json.load(f)

                    # 旧データはversionなし => v1として扱う
                    version = raw.get("version", 1)
                    users: dict = {}
                    migrated = False

                    if version == 1:
                        # v1→v2 変換: dataHashを空文字で初期化
                        for rec in raw.values():
                            rec["dataHash"] = ""
                        users = raw
                        migrated = True
                        logging.info("[CloudSaveStateStore] Migrated from v1 to v2")

                    elif version == 2:
                        users = raw.get("users", {})

                    else:
                        logging.warning(
                            f"[CloudSaveStateStore] Unknown version: {version}"
                        )
                        users = {}

                    for uid, rec in users.items():
                        self._data[uid] = CloudSaveState.model_validate(
                            rec, by_alias=True
                        )

                    if migrated:
                        self._save()

            except Exception as e:
                logging.warning(f"[CloudSaveStateStore] Failed to load: {e}")

    async def get(self, user_id: str) -> Optional[CloudSaveState]:
        async with self._lock:
            return self._data.get(user_id)

    async def update(self, user_id: str, state: CloudSaveState):
        async with self._lock:
            self._data[user_id] = state
            self._save()

    def _save(self):
        try:
            tmp = {
                uid: r.model_dump(mode="json", by_alias=True)
                for uid, r in self._data.items()
            }
            payload = {
                "version": self.VERSION,
                "users": tmp,
            }
            with open(self._path, "w", encoding="utf-8") as f:
                json.dump(payload, f, ensure_ascii=False, indent=2)
        except Exception as e:
            logging.error(f"[CloudSaveStateStore] Failed to save: {e}")
