from datetime import datetime


def linear_interpolate(
    t0: datetime, v0: float, t1: datetime, v1: float, target: datetime
) -> float:
    if t1 <= t0:
        return v0
    ratio = (target - t0).total_seconds() / (t1 - t0).total_seconds()
    return v0 + (v1 - v0) * ratio
