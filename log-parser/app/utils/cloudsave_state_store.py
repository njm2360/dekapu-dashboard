import json
import asyncio
import logging
from pathlib import Path
from datetime import datetime
from typing import Dict, Optional

from pydantic import BaseModel, ConfigDict, Field


class CloudSaveState(BaseModel):
    credit_all: int = Field(..., alias="creditAll")
    last_attempt_at: Optional[datetime] = Field(None, alias="lastAttemptAt")

    model_config = ConfigDict(populate_by_name=True)


class CloudSaveStateStore:
    def __init__(self, path: Path):
        self._path = path
        self._lock = asyncio.Lock()
        self._data: Dict[str, CloudSaveState] = {}

        if self._path.exists():
            try:
                with open(self._path, "r", encoding="utf-8") as f:
                    raw = json.load(f)
                    for uid, rec in raw.items():
                        self._data[uid] = CloudSaveState.model_validate(
                            rec, by_alias=True
                        )
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
            with open(self._path, "w", encoding="utf-8") as f:
                json.dump(tmp, f, ensure_ascii=False, indent=2)
        except Exception as e:
            logging.error(f"[CloudSaveStateStore] Failed to save: {e}")
