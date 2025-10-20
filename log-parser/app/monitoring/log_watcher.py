import asyncio
import logging
from pathlib import Path
from typing import Optional, TextIO
from datetime import datetime, timedelta, timezone
from aiohttp import ClientConnectorError
from influxdb_client import Point, WritePrecision

from app.model.mmp_savedata import MmpSaveRecord
from app.analysis.log_parser import MppLogParser, Event, ParseResult
from app.utils.offset_store import FileOffsetStore
from app.utils.influxdb import InfluxWriterAsync
from app.service.autosave_manager import AutoSaveManager
from app.analysis.medal_rate_ema import MedalRateEMA


class VRChatLogWatcher:
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

        self.last_timestamp: Optional[datetime] = None
        self.last_record: Optional[MmpSaveRecord] = None

        self.medal_rate = MedalRateEMA()

    async def run(self):
        file: Optional[TextIO] = None
        offset = await self.offset_store.get(self.fname)

        try:
            file = self._open_file_and_seek(offset)
            last_activity = datetime.now()

            logging.info(f"[Watcher] Start watching file={self.fname}, offset={offset}")

            while True:
                line = file.readline().strip()

                if not line:
                    await asyncio.sleep(1)

                    # 1時間更新がなければ監視を終了
                    if datetime.now() - last_activity > timedelta(hours=1):
                        logging.info(f"[Watcher] Stop watching {self.fname}")
                        await self.offset_store.remove(self.fname)
                        break
                    continue

                # オフセット更新
                await self.offset_store.set(self.fname, file.tell())
                last_activity = datetime.now()

                await self._process_line(line)

        except FileNotFoundError:
            logging.error(f"[Watcher] File not found: {self.fname}")
        except PermissionError:
            logging.error(f"[Watcher] Permission denied: {self.fname}")
        except asyncio.CancelledError:
            logging.info(f"[Watcher] Task cancelled: {self.fname}")
        except OSError as e:
            logging.error(f"[Watcher] File read error {self.fname}: {e}")

        finally:
            if file and not file.closed:
                file.close()

            await self._save_latest_record()

    def _open_file_and_seek(self, offset: Optional[int]) -> TextIO:
        file = open(self.log_file, "r", encoding="utf-8", errors="ignore")

        if offset is not None:
            file.seek(offset)
            logging.info(f"[Watcher] Resumed from offset (Pos:{offset}) ({self.fname})")
        else:
            file.seek(0, 2)
            logging.info(f"[Watcher] Skip to EOF (Pos:{file.tell()}) ({self.fname})")

        return file

    async def _process_line(self, line: str):
        result = self.parser.parse_line(line)
        if not result:
            return

        await self._handle_event(result)

    async def _handle_event(self, result: ParseResult):
        match result.event:
            case Event.TIMESTAMP_UPDATE:
                if ts := result.new_timestamp:
                    self.last_timestamp = ts

            case Event.DEKAPU_JP_STOCKOVER:
                if value := result.stockover_value:
                    self.medal_rate.add_offset(value)
                    logging.debug(f"[{self.fname}] JP stockover added: {value}")

            case Event.DEKAPU_CLOUD_LOAD:
                logging.info(f"[{self.fname}] Cloud load detected. Reset medal rate.")
                self.medal_rate.reset()

            case Event.DEKAPU_SESSION_RESET:
                logging.info(
                    f"[{self.fname}] Session reset detected. Reset medal rate."
                )
                self.medal_rate.reset()

            case Event.DEKAPU_WORLD_JOIN:
                logging.info(
                    f"[{self.fname}] Dekapu world join detected. Reset medal rate."
                )
                self.medal_rate.reset()

            case Event.DEKAPU_WORLD_LEAVE:
                logging.info(
                    f"[{self.fname}] Dekapu world leave detected. Saving latest record."
                )
                await self._save_latest_record()

            case Event.VRCHAT_APP_QUIT:
                logging.info(
                    f"[{self.fname}] VRChat app quit detected.. Saving latest record."
                )
                await self._save_latest_record()

            case Event.DEKAPU_SAVEDATA_UPDATE:
                if result.record:
                    await self._handle_savedata(result.record)

    async def _handle_savedata(self, record: MmpSaveRecord):
        self.last_record = record
        ts = self.last_timestamp or datetime.now(timezone.utc)

        point = (
            Point("mpp-savedata")
            .tag("user", record.user_id)
            .time(ts, WritePrecision.NS)
            .field("l_achieve_count", len(record.data.l_achieve or []))
        )

        credit_all = record.data.credit_all
        if credit_all is not None:
            if delta := self.medal_rate.update(credit_all, ts):
                point = point.field("credit_all_delta_1m", delta)
                logging.debug(f"[{self.fname}] Credit delta: {delta}/min")

        for k, v in record.data.model_dump_for_influx().items():
            point = point.field(k, v)

        try:
            await self.influx.write(point)
            logging.debug(f"[Watcher] Data write OK ({self.fname})")
        except (ClientConnectorError, asyncio.TimeoutError, OSError) as e:
            logging.error(f"[Watcher] InfluxDB write failed ({self.fname}): {e}")

        await self.autosave_mgr.update(record)

    async def _save_latest_record(self):
        if not self.last_record:
            return

        await self.autosave_mgr.update(self.last_record, ignore_rate_limit=True)
