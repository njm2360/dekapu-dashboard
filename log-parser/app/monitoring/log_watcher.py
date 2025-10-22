import asyncio
import logging
from pathlib import Path
from zoneinfo import ZoneInfo
from typing import Optional, TextIO
from datetime import datetime, timedelta
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
        self.influx_tasks: set[asyncio.Task] = set()
        self.autosave_mgr = autosave_mgr
        self.offset_store = offset_store
        self.parser = MppLogParser(self.fname)

        self.last_timestamp: Optional[datetime] = None
        self.last_record: Optional[MmpSaveRecord] = None

        self.medal_rate = MedalRateEMA()
        self.wait_leave_resume_url: bool = False

    async def run(self):
        file: Optional[TextIO] = None
        offset = await self.offset_store.get(self.fname)

        logging.info(f"[Watcher] Start watching file ({self.fname})")
        if offset is not None:
            logging.info(f"[Watcher] Found read offset (Pos: {offset}) ({self.fname})")

        try:
            file = self._open_file_and_seek(offset)
            last_activity = datetime.now()

            while True:
                line = file.readline().strip()

                if not line:
                    # 1時間更新がなければ監視を終了
                    if datetime.now() - last_activity > timedelta(hours=1):
                        logging.info(f"[Watcher] Stop watching file {self.fname}")
                        # ここでセーブするデータはないはずだが念の為セーブする
                        # (VRChat異常終了などでログが正常に出なかった場合など)
                        await self._save_latest_record()
                        break

                    await asyncio.sleep(1)
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

            if self.influx_tasks:
                logging.info(f"[Watcher] Waiting InfluxDB push tasks...")
                _, pending = await asyncio.wait(self.influx_tasks, timeout=5.0)
                if pending:
                    for t in pending:
                        t.cancel()

    def _open_file_and_seek(self, offset: Optional[int]) -> TextIO:
        file = open(self.log_file, "r", encoding="utf-8", errors="ignore")

        if offset is not None:
            file.seek(offset)
            logging.info(
                f"[Watcher] Resumed from offset (Pos: {offset}) ({self.fname})"
            )
        else:
            file.seek(0, 2)
            logging.info(f"[Watcher] Skip to EOF (Pos: {file.tell()}) ({self.fname})")

        return file

    async def _process_line(self, line: str):
        result = self.parser.parse_line(line)
        if not result:
            return

        await self._handle_event(result)

    async def _handle_event(self, result: ParseResult):
        match result.event:
            case Event.TIMESTAMP_UPDATE:
                if timestamp := result.new_timestamp:
                    self.last_timestamp = timestamp

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
                    f"[{self.fname}] Dekapu world leave detected. Waiting for leave save."
                )
                # Leave検出したあとに復帰用URLが発行されるのでこの地点では強制セーブしない
                self.wait_leave_resume_url = True

            case Event.VRCHAT_APP_QUIT:
                logging.info(
                    f"[{self.fname}] VRChat app quit detected.. Saving latest record."
                )
                await self._save_latest_record()

            case Event.DEKAPU_SAVEDATA_UPDATE:
                if (record := result.record) is None:
                    return
                self.last_record = record

                timestamp = self.last_timestamp or datetime.now(ZoneInfo("UTC"))

                delta = self.calc_medal_rate_ema(timestamp=timestamp, record=record)

                task = asyncio.create_task(
                    self._push_influxdb(
                        timestamp=timestamp,
                        record=record,
                        credit_all_delta_1m=delta,
                    )
                )
                self.influx_tasks.add(task)
                task.add_done_callback(self.influx_tasks.discard)

                # 退出時の復帰用URLはレート無視して保存
                if self.wait_leave_resume_url:
                    self.wait_leave_resume_url = False
                    await self.autosave_mgr.update(
                        record=record, ignore_rate_limit=True
                    )
                    return

                await self.autosave_mgr.update(record)

    def calc_medal_rate_ema(
        self, timestamp: datetime, record: MmpSaveRecord
    ) -> Optional[int]:
        credit_all = record.data.credit_all
        if credit_all is None:
            return

        delta = self.medal_rate.update(total=credit_all, timestamp=timestamp)
        if delta:
            logging.debug(f"[{self.fname}] Credit delta: {delta}/min")
        return delta

    async def _push_influxdb(
        self,
        timestamp: datetime,
        record: MmpSaveRecord,
        credit_all_delta_1m: Optional[int],
    ):
        point = (
            Point("mpp-savedata")
            .tag("user", record.user_id)
            .time(timestamp, WritePrecision.NS)
            .field("l_achieve_count", len(record.data.l_achieve or []))
        )

        if credit_all_delta_1m:
            point = point.field("credit_all_delta_1m", credit_all_delta_1m)

        for k, v in record.data.model_dump_for_influx().items():
            point = point.field(k, v)

        try:
            await self.influx.write(point)
            logging.debug(f"[Watcher] Data write OK ({self.fname})")
        except (ClientConnectorError, asyncio.TimeoutError, OSError) as e:
            logging.error(f"[Watcher] InfluxDB write failed ({self.fname}): {e}")

    async def _save_latest_record(self):
        if not self.last_record:
            return

        await self.autosave_mgr.update(self.last_record, ignore_rate_limit=True)
