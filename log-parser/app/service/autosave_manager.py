import asyncio
import aiohttp
import logging
from hashlib import sha256
from datetime import datetime, timedelta

from app.model.mmp_savedata import MmpSaveRecord, MmpSaveData
from app.utils.cloudsave_state_store import CloudSaveState, CloudSaveStateStore


class AutoSaveManager:
    def __init__(self, cloud_state_store: CloudSaveStateStore, save_interval: int):
        self._cloud_state_store = cloud_state_store
        self._save_interval = save_interval

        self._session = aiohttp.ClientSession(
            timeout=aiohttp.ClientTimeout(total=15),
            headers={"User-Agent": "dekapu-dashboard"},
        )
        self._pending_tasks: set[asyncio.Task] = set()

    async def close(self):
        if self._pending_tasks:
            await asyncio.gather(*self._pending_tasks, return_exceptions=True)

        await self._session.close()

    async def update(self, record: MmpSaveRecord, ignore_rate_limit: bool = False):
        credit_all = record.data.credit_all
        if credit_all is None:
            return

        now = datetime.now()
        user_id = record.user_id
        url = record.raw_url

        state = await self._cloud_state_store.get(user_id)

        if state:
            last_credit_all = state.credit_all
            last_attempt_at = state.last_attempt_at
            last_data_hash = state.data_hash

            # 成功・失敗にかかわらず、前回の試行時刻を基準にレート制御
            if not ignore_rate_limit and now - last_attempt_at < timedelta(
                seconds=self._save_interval
            ):
                logging.debug(
                    f"[CloudSave] Save skipped (rate limit exceed) (user={user_id})"
                )
                return

            # 総獲得が巻き戻っている場合はセーブしない
            if credit_all < last_credit_all:
                logging.warning(
                    f"[CloudSave] Data rollback detected - Skipping (user={user_id})"
                )
                return

            # データが同一の場合セーブしない
            data_hash = self._calc_data_hash(record.data)

            if data_hash == last_data_hash:
                logging.debug(
                    f"[CloudSave] Save skipped (no data change) (user={user_id})"
                )
                return

        else:
            data_hash = self._calc_data_hash(record.data)

        await self._cloud_state_store.update(
            user_id,
            CloudSaveState(
                credit_all=credit_all, last_attempt_at=now, data_hash=data_hash
            ),
        )

        # 非同期タスクでクラウドセーブ
        task = asyncio.create_task(self._do_request(url, user_id))
        self._pending_tasks.add(task)
        task.add_done_callback(self._pending_tasks.discard)

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

    def _calc_data_hash(self, data: MmpSaveData) -> str:
        data_json = data.model_dump_json()
        return sha256(data_json.encode("utf-8")).hexdigest()
