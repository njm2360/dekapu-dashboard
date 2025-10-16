import os
import signal
import asyncio
import logging
from pathlib import Path
from datetime import datetime
from zoneinfo import ZoneInfo
from dotenv import load_dotenv
from aiohttp import ClientConnectorError
from influxdb_client import Point, WritePrecision

from app.analysis.log_parser import MppLogParser
from app.utils.influxdb import InfluxWriterAsync
from app.utils.logger import setup_logger


load_dotenv(override=True)
setup_logger()


def prompt_datetime_input(prompt_text: str) -> datetime:
    while True:
        s = input(prompt_text).strip()
        if not s:
            return None
        try:
            dt_local = datetime.strptime(s, "%Y%m%d%H%M%S")
            dt_local = dt_local.replace(tzinfo=ZoneInfo("Asia/Tokyo"))

            dt_utc = dt_local.astimezone(ZoneInfo("UTC"))
            return dt_utc

        except ValueError:
            print("フォーマットが不正です。YYYYMMDDHHmmss 形式で入力してください。")


def confirm_settings(start_dt: datetime, end_dt: datetime):
    while True:
        tz = ZoneInfo("Asia/Tokyo")

        start_text = (
            start_dt.astimezone(tz).strftime("%Y-%m-%d %H:%M:%S")
            if start_dt
            else "制限なし"
        )
        end_text = (
            end_dt.astimezone(tz).strftime("%Y-%m-%d %H:%M:%S")
            if end_dt
            else "制限なし"
        )

        print(f"開始: {start_text}")
        print(f"終了: {end_text}")

        confirm = input("この設定で開始しますか？ [Y/n]: ").strip().lower()

        if confirm == "y":
            return True
        elif confirm == "n":
            print("処理を中止しました。")
            return False
        elif confirm == "":
            print("入力が空です。Y または n を入力してください。")
        else:
            print("無効な入力です。Y または n を入力してください。")


def collect_unique_txt_files(
    log_dir: Path, keep_duplicates: bool = False
) -> list[Path]:
    if keep_duplicates:
        files = [f for f in log_dir.rglob("*.txt") if f.is_file()]
        return sorted(files, key=lambda x: x.name)

    found = {}
    for f in log_dir.rglob("*.txt"):
        if f.is_file():
            name = f.name
            if name not in found:
                found[name] = f
    return [found[k] for k in sorted(found.keys())]


async def load_logs(
    log_dir: Path,
    influx: InfluxWriterAsync,
    start_dt: datetime | None,
    end_dt: datetime | None,
):
    logging.info(f"[Loader] Start loading logs from {log_dir}")

    files = collect_unique_txt_files(log_dir=log_dir)
    if not files:
        logging.warning(f"[Loader] No log files found in {log_dir}")
        return

    total_points = 0

    for log_file in files:
        fname = log_file.name
        parser = MppLogParser(fname)

        logging.info(f"[Loader] Processing {fname}")
        try:
            with open(log_file, "r", encoding="utf-8", errors="ignore") as f:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    record = parser.parse_line(line)
                    if not record:
                        continue

                    ts = record.timestamp
                    if start_dt and ts < start_dt:
                        continue
                    if end_dt and ts > end_dt:
                        continue

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

                    retry_count = 0
                    while retry_count < 3:
                        try:
                            await influx.write(point)
                            total_points += 1
                            break
                        except (
                            ClientConnectorError,
                            asyncio.TimeoutError,
                            OSError,
                        ) as e:
                            retry_count += 1
                            logging.warning(f"[Loader] InfluxDB write failed {e}")
                            await asyncio.sleep(3)
                    else:
                        logging.error(f"[Loader] InfluxDB write failed 3 times. Abort.")
                        raise RuntimeError("InfluxDB write failed 3 times, aborting.")

        except FileNotFoundError:
            logging.error(f"[Loader] File not found: {fname}")
        except PermissionError:
            logging.error(f"[Loader] Permission denied: {fname}")

    logging.info(f"[Loader] Finished. Written points: {total_points}")


async def main():
    start_dt = prompt_datetime_input("開始日時: ")
    end_dt = prompt_datetime_input("終了日時: ")

    if not confirm_settings(start_dt, end_dt):
        return

    influx = InfluxWriterAsync(
        os.getenv("INFLUXDB_URL"),
        os.getenv("INFLUXDB_TOKEN"),
        os.getenv("INFLUXDB_ORG"),
        os.getenv("INFLUXDB_BUCKET"),
    )
    log_dir = Path(os.getenv("VRCHAT_LOG_DIR", "/app/vrchat_log"))

    try:
        await load_logs(log_dir, influx, start_dt, end_dt)
    except Exception as e:
        logging.error(f"[Loader] Unexcept exception occured: {e}")
    finally:
        try:
            await influx.close()
        except Exception as e:
            logging.warning(f"[Loader] Failed to close InfluxDB session: {e}")


if __name__ == "__main__":

    def handle_sigint(signum, frame):
        raise KeyboardInterrupt

    signal.signal(signal.SIGINT, handle_sigint)

    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        pass
