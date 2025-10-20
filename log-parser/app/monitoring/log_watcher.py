import os
import asyncio
import logging
from pathlib import Path
from typing import Final, Optional
from datetime import datetime, timedelta
from aiohttp import ClientConnectorError
from influxdb_client import Point, WritePrecision

from app.model.mmp_savedata import MmpSaveRecord
from app.analysis.log_parser import MppLogParser
from app.utils.offset_store import FileOffsetStore
from app.utils.influxdb import InfluxWriterAsync
from app.service.autosave_manager import AutoSaveManager


class VRChatLogWatcher:
    ENABLE_AUTOSAVE: Final[bool] = bool("ENABLE_AUTOSAVE" in os.environ)  # 実験的機能

    def __init__(
        self,
        log_file: Path,
        influx: InfluxWriterAsync,
        autosave_mgr: AutoSaveManager,
        offset_store: FileOffsetStore,
    ):
        self.log_file = log_file
        self.fname = log_file.name
        self.influx = influx
        self.autosave_mgr = autosave_mgr
        self.offset_store = offset_store
        self.parser = MppLogParser(self.fname)
        self.last_record: Optional[MmpSaveRecord] = None

    async def run(self):
        offset = await self.offset_store.get(self.fname)

        logging.info(f"[Watcher] Start watching file={self.fname}, offset={offset}")

        try:
            with open(self.log_file, "r", encoding="utf-8", errors="ignore") as f:
                # オフセット情報をもとにシーク
                if offset is not None:
                    f.seek(offset)
                    logging.info(
                        f"[Watcher] Resumed from offset (Pos:{offset}) ({self.fname})"
                    )
                else:
                    f.seek(0, 2)
                    logging.info(
                        f"[Watcher] Skip to EOF (Pos:{f.tell()}) ({self.fname})"
                    )

                last_activity = datetime.now()

                while True:
                    try:
                        line = f.readline().strip()
                    except UnicodeDecodeError as e:
                        logging.warning(f"[Watcher] Decode error in {self.fname}: {e}")
                        continue
                    except OSError as e:
                        logging.error(f"[Watcher] File read error {self.fname}: {e}")
                        break

                    if not line:
                        await asyncio.sleep(1)

                        # 1時間更新がなければ監視を終了
                        if datetime.now() - last_activity > timedelta(hours=1):
                            logging.info(f"[Watcher] Stop watching {self.fname}")
                            await self.offset_store.remove(self.fname)
                            break
                        continue

                    # オフセット更新
                    await self.offset_store.set(self.fname, f.tell())
                    last_activity = datetime.now()

                    record = self.parser.parse_line(line.strip())
                    if not record:
                        continue

                    self.last_record = record

                    point = (
                        Point("mpp-savedata")
                        .tag("user", record.user_id)
                        .time(record.timestamp, WritePrecision.NS)
                        .field("l_achieve_count", len(record.data.l_achieve or []))
                    )

                    if record.credit_all_delta_1m is not None:
                        point = point.field(
                            "credit_all_delta_1m", record.credit_all_delta_1m
                        )

                    for k, v in record.data.model_dump_for_influx().items():
                        point = point.field(k, v)

                    try:
                        await self.influx.write(point)
                        logging.debug(f"[Watcher] Data write OK ({self.fname})")
                    except ClientConnectorError as e:
                        logging.error(f"[Watcher] InfluxDB connect error: {e}")
                    except asyncio.TimeoutError:
                        logging.error(
                            f"[Watcher] InfluxDB write timeout ({self.fname})"
                        )
                    except OSError as e:
                        logging.error(
                            f"[Watcher] InfluxDB write failed ({self.fname}): {e}"
                        )

                    if self.ENABLE_AUTOSAVE:
                        await self.autosave_mgr.update(record)

        except FileNotFoundError:
            logging.error(f"[Watcher] File not found: {self.fname}")
        except PermissionError:
            logging.error(f"[Watcher] Permission denied: {self.fname}")
        except asyncio.CancelledError:
            logging.info(f"[Watcher] Task cancelled: {self.fname}")

        finally:
            if self.last_record and self.ENABLE_AUTOSAVE:
                await self.autosave_mgr.update(
                    record=self.last_record,
                    ignore_rate_limit=True,
                )
