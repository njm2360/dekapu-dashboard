import json
import logging
from pathlib import Path
from typing import Optional


class FileOffsetStore:
    def __init__(self, filepath: Path):
        self._filepath = filepath
        self._filepath.parent.mkdir(parents=True, exist_ok=True)
        self._offsets: dict[str, int] = self._load()
        logging.info(
            f"[OffsetStore] Initialized with file={self._filepath}, "
            f"loaded {len(self._offsets)} entries"
        )

    def _load(self) -> dict[str, int]:
        if self._filepath.exists():
            try:
                with open(self._filepath, "r", encoding="utf-8") as f:
                    data = json.load(f)
                    logging.info(f"[OffsetStore] Loaded offsets from {self._filepath}")
                    return data
            except Exception as e:
                logging.warning(f"[OffsetStore] Failed to load offsets: {e}")
        else:
            logging.info(f"[OffsetStore] Offset file not found, starting empty")
        return {}

    def save(self):
        try:
            with open(self._filepath, "w", encoding="utf-8") as f:
                json.dump(self._offsets, f, ensure_ascii=False, indent=2)
            logging.info(
                f"[OffsetStore] Saved {len(self._offsets)} offsets to {self._filepath}"
            )
        except Exception as e:
            logging.error(f"[OffsetStore] Failed to save offsets: {e}")

    def get(self, filename: str) -> Optional[int]:
        return self._offsets.get(filename)

    def set(self, filename: str, offset: int) -> None:
        self._offsets[filename] = offset

    def remove(self, filename: str) -> None:
        self._offsets.pop(filename, None)

    def all(self) -> dict[str, int]:
        return dict(self._offsets)
