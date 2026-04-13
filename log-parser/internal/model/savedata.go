package model

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type FlexInt int64

func (f *FlexInt) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		parsed, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return fmt.Errorf("FlexInt: %w", err)
		}
		*f = FlexInt(parsed)
		return nil
	}
	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexInt(n)
		return nil
	}
	return fmt.Errorf("FlexInt: cannot parse %s", string(data))
}

type UnixTimestamp time.Time

func (t *UnixTimestamp) UnmarshalJSON(data []byte) error {
	var ts int64
	if err := json.Unmarshal(data, &ts); err == nil {
		*t = UnixTimestamp(time.Unix(ts, 0).UTC())
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		parsed, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return fmt.Errorf("UnixTimestamp: %w", err)
		}
		*t = UnixTimestamp(time.Unix(parsed, 0).UTC())
		return nil
	}
	return fmt.Errorf("UnixTimestamp: cannot parse %s", string(data))
}

func (t UnixTimestamp) Time() time.Time { return time.Time(t) }

type OverflowInt int64

func (o *OverflowInt) UnmarshalJSON(data []byte) error {
	var n int64
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	if n < 0 {
		n = n & 0xFFFFFFFF
	}
	*o = OverflowInt(n)
	return nil
}

type IntMap map[string]int

func (m *IntMap) UnmarshalJSON(data []byte) error {
	var raw map[string]*int
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	result := make(IntMap, len(raw))
	for k, v := range raw {
		if v != nil {
			result[k] = *v
		}
	}
	*m = result
	return nil
}

type Int64Map map[string]int64

func (m *Int64Map) UnmarshalJSON(data []byte) error {
	var raw map[string]*int64
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	result := make(Int64Map, len(raw))
	for k, v := range raw {
		if v != nil {
			result[k] = *v
		}
	}
	*m = result
	return nil
}

type MmpSaveData struct {
	Version    int           `json:"version"`     // バージョン
	Firstboot  UnixTimestamp `json:"firstboot"`   // 初回起動日時
	Lastsave   UnixTimestamp `json:"lastsave"`    // 最終保存日時
	HideRecord int           `json:"hide_record"` // ランキング非公開フラグ
	Credit     FlexInt       `json:"credit"`      // 現在のメダル所持数
	Playtime   int           `json:"playtime"`    // 総プレイ時間（秒）
	CreditAll  FlexInt       `json:"credit_all"`  // 累計メダル獲得数

	MedalIn      *OverflowInt `json:"medal_in,omitempty"`      // メダル投入総数
	MedalGet     *int64       `json:"medal_get,omitempty"`     // メダル獲得総数
	BallGet      *int64       `json:"ball_get,omitempty"`      // ボール獲得総数
	BallChain    *int         `json:"ball_chain,omitempty"`    // 最大ボールナンバー
	SlotStart    *int64       `json:"slot_start,omitempty"`    // ルーレット回転回数
	SlotStartfev *int64       `json:"slot_startfev,omitempty"` // フィーバー発生回数
	SlotHit      *int64       `json:"slot_hit,omitempty"`      // ルーレット当選回数
	SlotGetfev   *int64       `json:"slot_getfev,omitempty"`   // フィーバー時回転数
	SqrGet       *int64       `json:"sqr_get,omitempty"`       // すごろく進行回数
	SqrStep      *int64       `json:"sqr_step,omitempty"`      // すごろく合計マス数
	JackGet      *int64       `json:"jack_get,omitempty"`      // ジャックポット獲得数
	JackStartmax *int64       `json:"jack_startmax,omitempty"` // S-JPスタート値最高

	JackTotalmaxV2 *int64 `json:"jack_totalmax_v2,omitempty"` // S-JP最終結果最高（v2）
	JackTotalmax   *int   `json:"jack_totalmax,omitempty"`    // S-JP最終結果最高
	PallotLotT0    *int   `json:"pallot_lot_t0,omitempty"`    // パレッタ抽選（1段目）回数
	PallotLotT1    *int   `json:"pallot_lot_t1,omitempty"`    // パレッタ抽選（2段目）回数
	PallotLotT2    *int   `json:"pallot_lot_t2,omitempty"`    // パレッタ抽選（3段目）回数
	PallotLotT3    *int   `json:"pallot_lot_t3,omitempty"`    // パレッタ抽選（最終段）回数
	PallotLotT4    *int   `json:"pallot_lot_t4,omitempty"`    // パレッタ抽選（?????）回数
	JackspGetAll   *int   `json:"jacksp_get_all,omitempty"`   // パレッタJACKPOT獲得回数
	JackspGetT0    *int   `json:"jacksp_get_t0,omitempty"`    // シングルパレッタJACKPOT獲得回数
	JackspGetT1    *int   `json:"jacksp_get_t1,omitempty"`    // ダブルパレッタJACKPOT獲得回数
	JackspGetT2    *int   `json:"jacksp_get_t2,omitempty"`    // マッシブパレッタJACKPOT獲得回数
	JackspGetT3    *int   `json:"jacksp_get_t3,omitempty"`    // ヘブンパレッタJACKPOT獲得回数
	JackspGetT4    *int   `json:"jacksp_get_t4,omitempty"`    // ギャラクシーパレッタJACKPOT獲得回数
	JackspStartmax *int64 `json:"jacksp_startmax,omitempty"`  // P-JPスタート値最高
	JackspTotalmax *int64 `json:"jacksp_totalmax,omitempty"`  // P-JP最終結果最高

	UltGet        *OverflowInt `json:"ult_get,omitempty"`         // アルティメット回数
	UltCombomax   *int         `json:"ult_combomax,omitempty"`    // アルティメット最大コンボ数
	UltTotalmaxV2 *int64       `json:"ult_totalmax_v2,omitempty"` // アルティメット最終結果最高（v2）
	UltTotalmax   *int         `json:"ult_totalmax,omitempty"`    // アルティメット最終結果最高
	RmshbiGet     *OverflowInt `json:"rmshbi_get,omitempty"`      // お部屋シャルベクリック数
	BstpStep      *int64       `json:"bstp_step,omitempty"`       // ボーナスステップステップ数
	BstpRwd       *int64       `json:"bstp_rwd,omitempty"`        // ボーナスステップ報酬回数
	BuyTotal      *OverflowInt `json:"buy_total,omitempty"`       // ショップ購入総額
	BuyShbi       *OverflowInt `json:"buy_shbi,omitempty"`        // シャルベ救出数
	Sp            *int64       `json:"sp,omitempty"`              // SP所持数
	SpUse         *int64       `json:"sp_use,omitempty"`          // SP使用数
	CpmMax        *float64     `json:"cpm_max,omitempty"`         // 獲得速度最大値
	PalballGet    *int         `json:"palball_get,omitempty"`     // パレッタボール獲得数
	TaskCnt       *int         `json:"task_cnt,omitempty"`        // タスク完了回数

	DcMedalGet   IntMap   `json:"dc_medal_get,omitempty"`   // メダル獲得数詳細
	DcBallGet    Int64Map `json:"dc_ball_get,omitempty"`    // ボール獲得数詳細
	DcBallChain  IntMap   `json:"dc_ball_chain,omitempty"`  // ボールチェイン数詳細
	DcPalballGet IntMap   `json:"dc_palball_get,omitempty"` // パレッタボール獲得詳細
	DcPalballJp  IntMap   `json:"dc_palball_jp,omitempty"`  // P-JP当選時のボール詳細

	LPerks            []int64 `json:"l_perks,omitempty"`             // パークレベル一覧
	LPerksCredit      []int64 `json:"l_perks_credit,omitempty"`      // 各パークに対応する消費メダル量
	TotemAltars       *int    `json:"totem_altars,omitempty"`        // トーテム祭壇数
	TotemAltarsCredit *int64  `json:"totem_altars_credit,omitempty"` // 祭壇開放のための消費メダル量
	LTotems           []int   `json:"l_totems,omitempty"`            // トーテム獲得一覧
	LTotemsCredit     []int64 `json:"l_totems_credit,omitempty"`     // 各トーテムに対応する消費メダル量
	LTotemsSet        []int   `json:"l_totems_set,omitempty"`        // 設置済みトーテムリスト
	LAchieve          []int   `json:"l_achieve,omitempty"`           // 獲得済み実績一覧

	Legacy         *int   `json:"legacy,omitempty"`           // 旧仕様フラグ？
	Bbox           *int   `json:"bbox,omitempty"`             // 黒箱所持数
	BboxAll        *int64 `json:"bbox_all,omitempty"`         // 黒箱総獲得数
	BboxShop       *int   `json:"bbox_shop,omitempty"`        // 黒箱ショップ利用回数
	BboxUsedFerlot *int   `json:"bbox_used_ferlot,omitempty"` // フェレッタで使用した黒箱数？

	JackfrGetAll   *int   `json:"jackfr_get_all,omitempty"`  // フェレッタJACKPOT獲得回数
	JackfrGetT0    *int   `json:"jackfr_get_t0,omitempty"`   // シングルフェレッタJACKPOT獲得回数
	JackfrGetT1    *int   `json:"jackfr_get_t1,omitempty"`   // ダブルフェレッタJACKPOT獲得回数
	JackfrGetT2    *int   `json:"jackfr_get_t2,omitempty"`   // マッシブフェレッタJACKPOT獲得回数
	JackfrGetT3    *int   `json:"jackfr_get_t3,omitempty"`   // ヘブンフェレッタJACKPOT獲得回数
	JackfrGetT4    *int   `json:"jackfr_get_t4,omitempty"`   // ユニバースフェレッタJACKPOT獲得回数
	JackfrStartmax *int64 `json:"jackfr_startmax,omitempty"` // F-JPスタート値最高
	JackfrTotalmax *int64 `json:"jackfr_totalmax,omitempty"` // F-JP最終結果最高

	FerballGet   *int `json:"ferball_get,omitempty"`   // フェレッタボール獲得数
	FerlotAct    *int `json:"ferlot_act,omitempty"`    // ?
	FerlotChance *int `json:"ferlot_chance,omitempty"` // アイテムポケット入賞
	FerlotHit    *int `json:"ferlot_hit,omitempty"`    // ビンゴカード獲得ナンバー数
	FerlotLines  *int `json:"ferlot_lines,omitempty"`  // ビンゴカード獲得ライン数
	FerlotLose   *int `json:"ferlot_lose,omitempty"`   // フェレッタ抽選はずれ回数
	FerlotLot    *int `json:"ferlot_lot,omitempty"`    // フェレッタチャンス挑戦回数
	FerlotMaxln  *int `json:"ferlot_maxln,omitempty"`  // ビンゴカード最大同時ライン数

	DcBboxShop      IntMap `json:"dc_bbox_shop,omitempty"`      // 種類別アイテム購入回数
	DcFerlotItem    IntMap `json:"dc_ferlot_item,omitempty"`    // 種類別アイテムポケット獲得回数
	DcFerlotUseitem IntMap `json:"dc_ferlot_useitem,omitempty"` // 種類別アイテムポケット使用数

	GetMedalTower *int `json:"get_medaltower,omitempty"` // メダルタワー獲得数
}

func (d *MmpSaveData) DumpForInflux() map[string]any {
	m := make(map[string]any)

	rv := reflect.ValueOf(d).Elem()
	rt := rv.Type()

	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		key := strings.SplitN(tag, ",", 2)[0]

		fv := rv.Field(i)

		if fv.Kind() == reflect.Pointer {
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
		}

		switch fv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			m[key] = fv.Int()
		case reflect.Float32, reflect.Float64:
			m[key] = fv.Float()
		case reflect.Map:
			if fv.IsNil() || !strings.HasPrefix(key, "dc_") {
				continue
			}
			for _, mk := range fv.MapKeys() {
				m[key+"_"+mk.String()] = fv.MapIndex(mk).Int()
			}
		case reflect.Slice:
			if fv.IsNil() || key != "l_totems_set" {
				continue
			}
			for j := 0; j < fv.Len(); j++ {
				m[fmt.Sprintf("%s_%d", key, j+1)] = fv.Index(j).Int()
			}
		}
	}

	return m
}

type MmpSaveRecord struct {
	Data   *MmpSaveData
	UserID string
	Sig    string
	RawURL string
}
