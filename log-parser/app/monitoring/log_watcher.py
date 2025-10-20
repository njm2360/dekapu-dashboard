import os
import asyncio
import logging
from pathlib import Path
from typing import Final, Optional
from datetime import datetime, timedelta, timezone
from aiohttp import ClientConnectorError
from influxdb_client import Point, WritePrecision

from app.model.mmp_savedata import MmpSaveRecord
from app.analysis.log_parser import MppLogParser, Event
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

        self.stock_over = 0
        self.medal_rate = MedalRateEMA()

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

                    result = self.parser.parse_line(line.strip())
                    if not result:
                        continue

                    match result.event:
                        case Event.TIMESTAMP_UPDATE:
                            if ts := result.new_timestamp:
                                self.last_timestamp = ts
                        case Event.DEKAPU_JP_STOCKOVER:
                            if value := result.stockover_value:
                                self.stock_over += value
                                logging.debug(
                                    f"[{self.fname}] JP stockover added: {value}"
                                )
                        case Event.DEKAPU_CLOUD_LOAD:
                            logging.info(
                                f"[{self.fname}] Cloud load detected. Reset medal rate."
                            )
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
                            logging.info(f"[{self.fname}] Dekapu world leave detected.")
                            await self.autosave_mgr.update(
                                self.last_record, ignore_rate_limit=True
                            )
                        case Event.VRCHAT_APP_QUIT:
                            logging.info(f"[{self.fname}] VRChat app quit detected.")
                            await self.autosave_mgr.update(
                                self.last_record, ignore_rate_limit=True
                            )
                            break  # このログにはもう追記されないためタスク終了

                        case Event.DEKAPU_SAVEDATA_UPDATE:
                            if result.record is None:
                                continue

                            record = result.record
                            self.last_record = record

                            # タイムスタンプが未取得の場合は現在時刻とする
                            # Memo: データ内のlastsaveがセーブURL生成時刻かも?
                            ts = self.last_timestamp or datetime.now(timezone.utc)
                            if not self.last_timestamp:
                                logging.warning(
                                    f"[{self.fname}] No timestamp captured, fallback to now."
                                )

                            point = (
                                Point("mpp-savedata")
                                .tag("user", record.user_id)
                                .time(ts, WritePrecision.NS)
                                .field(
                                    "l_achieve_count", len(record.data.l_achieve or [])
                                )
                            )

                            credit_all = record.data.credit_all
                            if credit_all is not None:
                                # ストック溢れ分を差し引いて増加量を計算
                                fixed_credit = credit_all - self.stock_over
                                delta = self.medal_rate.update(fixed_credit, ts)
                                if delta is not None:
                                    point = point.field("credit_all_delta_1m", delta)
                                    logging.debug(
                                        f"[{self.fname}] Credit delta: {delta}/min"
                                    )

                            for k, v in record.data.model_dump_for_influx().items():
                                point = point.field(k, v)

                            # InfluxDBプッシュ
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

                            # 自動クラウドセーブ
                            await self.autosave_mgr.update(record)

        except FileNotFoundError:
            logging.error(f"[Watcher] File not found: {self.fname}")
        except PermissionError:
            logging.error(f"[Watcher] Permission denied: {self.fname}")
        except asyncio.CancelledError:
            logging.info(f"[Watcher] Task cancelled: {self.fname}")

        finally:
            if self.last_record:
                await self.autosave_mgr.update(
                    record=self.last_record,
                    ignore_rate_limit=True,
                )
