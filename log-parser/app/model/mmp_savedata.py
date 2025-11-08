import logging
from datetime import datetime, timezone
from typing import Optional, Dict, List, Any
from pydantic import BaseModel, ConfigDict, field_validator


class MmpSaveData(BaseModel):
    version: int  # バージョン
    firstboot: datetime  # 初回起動日時
    lastsave: datetime  # 最終保存日時
    hide_record: int  # ランキング非公開
    credit: int  # 現在のメダル所持数
    playtime: int  # 総プレイ時間（秒）
    credit_all: int  # 累計メダル獲得数
    medal_in: Optional[int] = None  # メダル投入総数
    medal_get: Optional[int] = None  # メダル獲得総数
    ball_get: Optional[int] = None  # ボール獲得総数
    ball_chain: Optional[int] = None  # 最大ボールナンバー
    slot_start: Optional[int] = None  # ルーレット回転回数
    slot_startfev: Optional[int] = None  # フィーバー発生回数
    slot_hit: Optional[int] = None  # ルーレット当選回数
    slot_getfev: Optional[int] = None  # フィーバー時回転数
    sqr_get: Optional[int] = None  # すごろく進行回数
    sqr_step: Optional[int] = None  # すごろく合計マス数
    jack_get: Optional[int] = None  # ジャックポット獲得数
    jack_startmax: Optional[int] = None  # S-JPスタート値最高
    jack_totalmax_v2: Optional[int] = None  # S-JP最終結果最高（v2）
    jack_totalmax: Optional[int] = None  # S-JP最終結果最高（旧形式）
    pallot_lot_t0: Optional[int] = None  # パレッタ抽選（1段目）回数
    pallot_lot_t1: Optional[int] = None  # パレッタ抽選（2段目）回数
    pallot_lot_t2: Optional[int] = None  # パレッタ抽選（3段目）回数
    pallot_lot_t3: Optional[int] = None  # パレッタ抽選（最終段）回数
    pallot_lot_t4: Optional[int] = None  # パレッタ抽選（?????）回数
    jacksp_get_all: Optional[int] = None  # パレッタJACKPOT獲得回数
    jacksp_get_t0: Optional[int] = None  # シングルパレッタJACKPOT獲得回数
    jacksp_get_t1: Optional[int] = None  # ダブルパレッタJACKPOT獲得回数
    jacksp_get_t2: Optional[int] = None  # マッシブパレッタJACKPOT獲得回数
    jacksp_get_t3: Optional[int] = None  # へブンパレッタJACKPOT獲得回数
    jacksp_get_t4: Optional[int] = None  # ????????????JACKPOT獲得回数
    jacksp_startmax: Optional[int] = None  # P-JPスタート値最高
    jacksp_totalmax: Optional[int] = None  # P-JP最終結果最高
    ult_get: Optional[int] = None  # アルティメット回数
    ult_combomax: Optional[int] = None  # アルティメット最大コンボ数
    ult_totalmax_v2: Optional[int] = None  # アルティメット最終結果最高（v2）
    ult_totalmax: Optional[int] = None  # アルティメット最終結果最高（旧形式）
    rmshbi_get: Optional[int] = None  # シャルベ救出報酬取得数？
    bstp_step: Optional[int] = None  # ボーナスステップステップ数
    bstp_rwd: Optional[int] = None  # ボーナスステップ報酬回数
    buy_total: Optional[int] = None  # ショップ購入総額
    buy_shbi: Optional[int] = None  # シャルベ救出数
    sp: Optional[int] = None  # SP所持数
    sp_use: Optional[int] = None  # SP使用数
    cpm_max: Optional[float] = None  # 獲得速度最大値
    palball_get: Optional[int] = None  # パレッタボール獲得数
    task_cnt: Optional[int] = None  # タスク完了回数

    dc_medal_get: Optional[Dict[str, int]] = None  # メダル獲得数詳細
    dc_ball_get: Optional[Dict[str, int]] = None  # ボール獲得数詳細
    dc_ball_chain: Optional[Dict[str, int]] = None  # ボールチェイン数詳細
    dc_palball_get: Optional[Dict[str, int]] = None  # パレッタボール獲得詳細
    dc_palball_jp: Optional[Dict[str, int]] = None  # P-JP当選時のボール詳細

    l_perks: Optional[List[int]] = None  # パークレベル一覧
    l_perks_credit: Optional[List[int]] = None  # 各パークに対応する消費メダル量
    totem_altars: Optional[int] = None  # トーテム祭壇数
    totem_altars_credit: Optional[int] = None  # 祭壇開放のための消費メダル量
    l_totems: Optional[List[int]] = None  # トーテム獲得一覧
    l_totems_credit: Optional[List[int]] = None  # 各トーテムに対応する消費メダル量
    l_totems_set: Optional[List[int]] = None  # 設置済みトーテムリスト
    l_achieve: Optional[List[int]] = None  # 獲得済み実績一覧

    legacy: Optional[int] = None  # 旧仕様フラグ？

    bbox: Optional[int] = None  # 黒箱所有数
    bbox_all: Optional[int] = None  # 黒箱獲得数
    # blackbox_credits: Optional[int] = None  # 黒箱(Legacy)

    model_config = ConfigDict(extra="ignore")  # 未知フィールドは破棄する

    @field_validator("firstboot", "lastsave", mode="before")
    def convert_unix_to_datetime(cls, v: Any):
        if not isinstance(v, (str, int)):
            return None

        try:
            ts = int(v)
            return datetime.fromtimestamp(ts, tz=timezone.utc)
        except ValueError:
            logging.warning(f"Invalid Unix timestamp: {v}")

    def model_dump_for_influx(self) -> dict:
        data = self.model_dump(exclude_none=True)
        flat = {}

        for k, v in data.items():
            # スカラー型（int, float, str）はそのまま
            if isinstance(v, (int, float, str)):
                flat[k] = v

            # dict型（dc_xx系）はキーを結合して平坦化
            elif isinstance(v, dict) and k.startswith("dc_"):
                for sub_k, sub_v in v.items():
                    flat[f"{k}_{sub_k}"] = sub_v

            # list型（l_totems_set系）は添字付きで展開
            elif isinstance(v, list) and k.startswith("l_totems_set"):
                for i, item in enumerate(v, start=1):
                    flat[f"{k}_{i}"] = item

        return flat


class MmpSaveRecord(BaseModel):
    data: MmpSaveData  # データ
    user_id: str  # ユーザー名
    sig: str  # シグネチャ
    raw_url: str  # 生のセーブURL
