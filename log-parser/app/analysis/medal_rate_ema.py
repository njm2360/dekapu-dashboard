import math
from datetime import datetime
from typing import Optional


class MedalRateEMA:
    def __init__(self, decay_const: float = 20.0):
        self.decay_const = decay_const
        self.last_timestamp: Optional[datetime] = None
        self.last_total: Optional[int] = None
        self.ema_rate: float = 0.0

    def update(self, total: int, timestamp: datetime) -> Optional[int]:
        if self.last_timestamp is None:
            self.last_timestamp = timestamp
            self.last_total = total
            return None

        dt = (timestamp - self.last_timestamp).total_seconds()
        if dt <= 0:
            return int(self.ema_rate)

        delta = total - self.last_total
        rate_instant = (delta / dt) * 60.0  # [medals/min]

        alpha = 1 - math.exp(-dt / self.decay_const)

        self.ema_rate = (1 - alpha) * self.ema_rate + alpha * rate_instant

        self.last_timestamp = timestamp
        self.last_total = total

        return int(self.ema_rate)

    def reset(self) -> None:
        self.last_timestamp = None
        self.last_total = None
        self.ema_rate = 0.0
