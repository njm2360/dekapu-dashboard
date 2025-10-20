import asyncio
import aiohttp
import logging
from datetime import datetime, timedelta

from app.utils.cloudsave_state_store import CloudSaveState, CloudSaveStateStore


class AutoSaveManager:
    def __init__(
        self, cloud_state_store: CloudSaveStateStore, save_interval: int = 300
    ):
        self._cloud_state_store = cloud_state_store
        self._save_interval = save_interval

        self._session = aiohttp.ClientSession(
            timeout=aiohttp.ClientTimeout(total=15),
            headers={"User-Agent": "dekapu-dashboard"},
        )

    async def close(self):
        await self._session.close()

    async def update(self, record: MmpSaveRecord, ignore_rate_limit: bool = False):
        credit_all = record.data.credit_all
        if credit_all is None:
            return

        now = datetime.now()

        record = await self._cloud_state_store.get(user_id)

        if record:
            last_credit_all = record.credit_all
            last_attempt_at = record.last_attempt_at or datetime.min

            # 成功・失敗にかかわらず、前回の試行時刻を基準にレート制御
            if now - last_attempt_at < timedelta(seconds=self._save_interval):
                return

            # 総獲得が巻き戻っている場合はセーブしない
            if credit_all < last_credit_all:
                logging.warning(
                    f"[CloudSave] Data rollback detected - Skiping (user={user_id})"
                )
                return

        await self._cloud_state_store.update(
            user_id,
            CloudSaveState(
                credit_all=credit_all,
                last_attempt_at=now,
            ),
        )

        # 非同期タスクでクラウドセーブ
        asyncio.create_task(self._do_request(url, user_id))

    async def _do_request(self, url: str, user_id: str):
        try:
            async with self._session.get(url) as resp:
                text = (await resp.text()).strip()

                if resp.status == 200:
                    logging.info(f"[CloudSave] Success (user={user_id})")
                elif 400 <= resp.status < 500:
                    logging.warning(
                        f"[CloudSave] Client error {resp.status} — {text[:200]}"
                    )
                elif resp.status >= 500:
                    logging.warning(
                        f"[CloudSave] Server error {resp.status} — {text[:200]}"
                    )
                else:
                    logging.warning(f"[CloudSave] Unhandled status {resp.status}")

        except aiohttp.ClientConnectorError as e:
            logging.error(f"[CloudSave] Connection failed: {e}")
        except asyncio.TimeoutError:
            logging.error(f"[CloudSave] Request timed out")
        except Exception as e:
            logging.exception(f"[CloudSave] Unexpected error occured: {e}")
