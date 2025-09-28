import os
import sys
import signal
import asyncio
import logging
from pathlib import Path
from dotenv import load_dotenv

from app.monitoring.log_watcher import VRChatLogWatcher
from app.utils.influxdb import InfluxWriterAsync
from app.utils.logger import setup_logger


load_dotenv(override=True)
setup_logger()


async def main():
    influx = InfluxWriterAsync(
        os.getenv("INFLUXDB_URL"),
        os.getenv("INFLUXDB_TOKEN"),
        os.getenv("INFLUXDB_ORG"),
        os.getenv("INFLUXDB_BUCKET"),
    )
    watcher = VRChatLogWatcher(
        Path(os.getenv("VRCHAT_LOG_DIR", "/app/vrchat_log")), influx,
    )

    loop = asyncio.get_running_loop()
    stop_event = asyncio.Event()

    def _signal_handler():
        logging.info("Received stop signal, shutting down...")
        stop_event.set()

    if sys.platform != "win32":
        loop.add_signal_handler(signal.SIGTERM, _signal_handler)
        loop.add_signal_handler(signal.SIGINT, _signal_handler)

    watcher_task = asyncio.create_task(watcher.run())
    stop_task = asyncio.create_task(stop_event.wait())

    try:
        _, pending = await asyncio.wait(
            [watcher_task, stop_task],
            return_when=asyncio.FIRST_COMPLETED,
        )
        for task in pending:
            task.cancel()
    finally:
        await influx.close()


if __name__ == "__main__":
    asyncio.run(main())
