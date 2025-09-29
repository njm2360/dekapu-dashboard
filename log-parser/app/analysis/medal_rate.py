from collections import deque
from datetime import datetime, timedelta
from typing import Deque, Tuple, Optional

from app.utils.interpolation import linear_interpolate


class MedalRate:
    def __init__(self):
        self.history: Deque[Tuple[datetime, int]] = deque()

    def update(self, credit_all: int, timestamp: datetime) -> Optional[int]:
        self.history.append((timestamp, credit_all))

        # 2分より古いデータを削除
        cutoff = timestamp - timedelta(minutes=2)
        while self.history and self.history[0][0] < cutoff:
            self.history.popleft()

        # 1分前の値を線形補間
        one_min_ago = timestamp - timedelta(minutes=1)
        before, after = None, None
        for t, v in self.history:
            if t <= one_min_ago:
                before = (t, v)
            if t >= one_min_ago and after is None:
                after = (t, v)

        if before and after:
            interpolated = linear_interpolate(
                before[0], before[1], after[0], after[1], one_min_ago
            )
            return int(credit_all - interpolated)

        return None
