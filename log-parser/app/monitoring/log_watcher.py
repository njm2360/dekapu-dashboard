import asyncio
import logging
from pathlib import Path
from datetime import datetime, timedelta

from aiohttp import ClientConnectorError
from influxdb_client import Point, WritePrecision

from app.analysis.log_parser import MppLogParser
from app.utils.offset_store import FileOffsetStore
from app.utils.influxdb import InfluxWriterAsync


class VRChatLogWatcher:
    def __init__(self, log_dir: Path, influx: InfluxWriterAsync):
        self.log_dir = log_dir
        self.influx = influx
        self.parsers: dict[str, MppLogParser] = {}
        self.offset_store = FileOffsetStore(Path("data") / "offsets.json")

        logging.info(f"[Watcher] Initialized. Log directory={log_dir}")

    async def _cleanup_offsets(self):
        logging.info(f"[Watcher] Cleamup stale offset entrys")
        existing_files = {f.name for f in self.log_dir.glob("output_log_*.txt")}
        for fname in list(self.offset_store.all().keys()):
            if fname not in existing_files:
                logging.info(f"[Watcher] Removing stale offset entry: {fname}")
                self.offset_store.remove(fname)

    async def watch_file(self, log_file: Path):
        fname = log_file.name
        parser = self.parsers.setdefault(fname, MppLogParser(fname))
        offset = self.offset_store.get(fname)

        logging.info(f"[Watcher] Start watching file={fname}, offset={offset}")

        try:
            with open(log_file, "r", encoding="utf-8", errors="ignore") as f:
                # オフセット情報をもとにシーク
                if offset is not None:
                    f.seek(offset)
                    logging.info(f"[Watcher] Resumed from offset {offset} ({fname})")
                else:
                    f.seek(0, 2)
                    logging.info(f"[Watcher] Skip to EOF (no offset) ({fname})")

                last_activity = datetime.now()

                while True:
                    try:
                        line = f.readline().strip()
                    except UnicodeDecodeError as e:
                        logging.warning(f"[Watcher] Decode error in {fname}: {e}")
                        continue
                    except OSError as e:
                        logging.error(f"[Watcher] File read error {fname}: {e}")
                        break

                    if not line:
                        await asyncio.sleep(1)

                        # 1時間更新がなければ監視を終了
                        if datetime.now() - last_activity > timedelta(hours=1):
                            logging.info(f"[Watcher] Stop watching {fname}")
                            self.offset_store.remove(fname)
                            break
                        continue

                    # オフセット更新
                    self.offset_store.set(fname, f.tell())
                    last_activity = datetime.now()

                    record = parser.parse_line(line.strip())

                    if not record:
                        continue

                    point = (
                        Point("mpp-savedata")
                        .tag("user", record.user_id)
                        .time(record.timestamp, WritePrecision.NS)
                        .field("l_achieve_count", len(record.data.l_achieve or []))
                    )

                    if record.credit_all_delta_1m is not None:
                        point = point.field("credit_all_delta_1m", record.credit_all_delta_1m)

                    for k, v in record.data.model_dump_for_influx().items():
                        point = point.field(k, v)

                    try:
                        await self.influx.write(point)
                        logging.debug(f"[Watcher] Data write OK ({fname})")
                    except ClientConnectorError as e:
                        logging.error(f"[Watcher] InfluxDB connect error: {e}")
                        await asyncio.sleep(5)
                        continue
                    except asyncio.TimeoutError:
                        logging.error(f"[Watcher] InfluxDB write timeout ({fname})")
                        await asyncio.sleep(5)
                        continue
                    except OSError as e:
                        logging.error(f"[Watcher] InfluxDB write failed ({fname}): {e}")
                        await asyncio.sleep(5)
                        continue

        except FileNotFoundError:
            logging.error(f"[Watcher] File not found: {fname}")
        except PermissionError:
            logging.error(f"[Watcher] Permission denied: {fname}")

    async def run(self):
        tasks: dict[str, asyncio.Task] = {}

        try:
            while True:
                for log_file in self.log_dir.glob("output_log_*.txt"):
                    if not log_file.is_file():
                        continue

                    # 1時間以上更新されていないファイルは無視
                    if datetime.fromtimestamp(
                        log_file.stat().st_mtime
                    ) < datetime.now() - timedelta(hours=1):
                        continue

                    if log_file.name not in tasks or tasks[log_file.name].done():
                        tasks[log_file.name] = asyncio.create_task(
                            self.watch_file(log_file)
                        )

                await asyncio.sleep(10)

        finally:
            await self._cleanup_offsets()
            self.offset_store.save()
