import json
import asyncio
import logging
from pathlib import Path
from typing import Optional


class FileOffsetStore:
    def __init__(self, path: Path, autosave_interval: int = 60):
        self._path = path
        self._path.parent.mkdir(parents=True, exist_ok=True)
        self._lock = asyncio.Lock()
        self._offsets: dict[str, int] = self._load()
        self._dirty = False
        self._autosave_interval = autosave_interval
        self._autosave_task = asyncio.create_task(self._autosave_loop())

        logging.info(
            f"[OffsetStore] Initialized with file={self._path}, "
            f"loaded {len(self._offsets)} entries"
        )

    def _load(self) -> dict[str, int]:
        if self._path.exists():
            try:
                with open(self._path, "r", encoding="utf-8") as f:
                    data = json.load(f)
                    if not isinstance(data, dict):
                        raise ValueError("Invalid offset file structure")
                    logging.info(f"[OffsetStore] Loaded offsets from {self._path}")
                    return {k: int(v) for k, v in data.items()}
            except Exception as e:
                logging.warning(f"[OffsetStore] Failed to load offsets: {e}")
        else:
            logging.info(f"[OffsetStore] Offset file not found, starting empty")
        return {}

    async def _save(self) -> None:
        try:
            tmp = self._path.with_suffix(".tmp")
            async with self._lock:
                with open(tmp, "w", encoding="utf-8") as f:
                    json.dump(self._offsets, f, ensure_ascii=False, indent=2)
                tmp.replace(self._path)
                self._dirty = False
            logging.debug(f"[OffsetStore] Saved {len(self._offsets)} offsets")
        except Exception as e:
            logging.error(f"[OffsetStore] Failed to save offsets: {e}")

    async def _autosave_loop(self):
        try:
            while True:
                await asyncio.sleep(self._autosave_interval)
                if self._dirty:
                    await self._save()
        except asyncio.CancelledError:
            logging.info("[OffsetStore] Autosave task cancelled gracefully")

    async def get(self, filename: str) -> Optional[int]:
        async with self._lock:
            return self._offsets.get(filename)

    async def set(self, filename: str, offset: int) -> None:
        async with self._lock:
            self._offsets[filename] = offset
            self._dirty = True

    async def remove(self, filename: str) -> None:
        async with self._lock:
            self._offsets.pop(filename, None)
            self._dirty = True

    async def all(self) -> dict[str, int]:
        async with self._lock:
            return dict(self._offsets)

    async def flush(self):
        logging.info("[OffsetStore] Flushing and stopping autosave task")
        if self._autosave_task:
            self._autosave_task.cancel()
            try:
                await self._autosave_task
            except asyncio.CancelledError:
                pass
        await self._save()
