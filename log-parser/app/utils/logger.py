import logging
from pathlib import Path
from datetime import datetime


def setup_logger():
    local_log_dir = Path("logs")
    local_log_dir.mkdir(parents=True, exist_ok=True)
    start_time = datetime.now().strftime("%Y%m%d_%H%M%S")
    local_log_file = local_log_dir / f"{start_time}.log"

    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
        handlers=[
            logging.FileHandler(local_log_file, encoding="utf-8"),
            logging.StreamHandler(),
        ],
        force=True,
    )
