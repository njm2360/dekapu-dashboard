import asyncio
import logging
from typing import Final
from pathlib import Path
from datetime import datetime, timedelta

from app.utils.offset_store import FileOffsetStore
from app.utils.influxdb import InfluxWriterAsync
from app.service.autosave_manager import AutoSaveManager
from app.utils.cloudsave_state_store import CloudSaveStateStore
from app.monitoring.log_watcher import VRChatLogWatcher


class LogWatcherManager:
    LOG_FILE_GLOB: Final[str] = "output_log_*.txt"
    OFFSET_STORE_FILE: Final[str] = "offsets.json"
    CLOUD_STATE_FILE: Final[str] = "cloudsave.json"

    # Note: pikachu0310さん協力の元の調整した数値です。サーバー負荷に繋がるので変更しないこと。
    AUTOSAVE_INTERVAL: Final[int] = 1800

    def __init__(self, log_dir: Path, data_dir: Path, influx: InfluxWriterAsync):
        self.log_dir = log_dir
        self.influx = influx
        self.offset_store = FileOffsetStore(path=data_dir / self.OFFSET_STORE_FILE)
        self.cloud_state_store = CloudSaveStateStore(
            path=data_dir / self.CLOUD_STATE_FILE
        )
        self.autosave_mgr = AutoSaveManager(
            cloud_state_store=self.cloud_state_store,
            save_interval=self.AUTOSAVE_INTERVAL,
        )
        self.tasks: dict[str, asyncio.Task] = {}

        logging.info(f"[Manager] Initialized. Log directory={log_dir}")

    async def _cleanup_offsets(self):
        logging.info("[Manager] Cleanup stale offset entries")
        try:
            existing_files = {f.name for f in self.log_dir.glob(self.LOG_FILE_GLOB)}
            offsets = await self.offset_store.all()
            for fname in set(offsets) - existing_files:
                logging.info(f"[Manager] Removing stale offset entry: {fname}")
                await self.offset_store.remove(fname)
        except Exception as e:
            logging.warning(f"[Manager] Cleanup failed: {e}")

    async def run(self):
        try:
            while True:
                for log_file in self.log_dir.glob(self.LOG_FILE_GLOB):
                    if not log_file.is_file():
                        continue

                    # 1時間以上更新されていないファイルはスキップ
                    if datetime.fromtimestamp(
                        log_file.stat().st_mtime
                    ) < datetime.now() - timedelta(hours=1):
                        continue

                    fname = log_file.name

                    if fname not in self.tasks or self.tasks[fname].done():
                        watcher = VRChatLogWatcher(
                            log_file=log_file,
                            influx=self.influx,
                            autosave_mgr=self.autosave_mgr,
                            offset_store=self.offset_store,
                        )
                        self.tasks[fname] = asyncio.create_task(
                            coro=watcher.run(), name=f"[Watcher] {fname}"
                        )

                await asyncio.sleep(10)

        except asyncio.CancelledError:
            logging.info("[Manager] File watch task cancelled.")

        finally:
            for name, task in self.tasks.items():
                if not task.done():
                    logging.info(f"[Manager] Cancelling watch task: {name}")
                    task.cancel()

            await asyncio.gather(*self.tasks.values(), return_exceptions=True)

            await self._cleanup_offsets()
            await self.offset_store.flush()
            await self.autosave_mgr.close()
