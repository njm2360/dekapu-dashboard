package handler

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/influxdata/influxdb-client-go/v2/api/write"

	"log-parser/internal/model"
	"log-parser/internal/watcher"
)

// --- モック ---

type mockWriter struct {
	count int
}

func (w *mockWriter) WritePoint(_ *write.Point) { w.count++ }

type mockSaveManager struct {
	calls     []saveCall
	returnVal bool
}

type saveCall struct {
	record          *model.MmpSaveRecord
	ignoreRateLimit bool
}

func (m *mockSaveManager) TrySave(record *model.MmpSaveRecord, ignoreRateLimit bool) bool {
	m.calls = append(m.calls, saveCall{record, ignoreRateLimit})
	return m.returnVal
}

// --- ヘルパー ---

const (
	lineCloudLoad    = "[LoadFromParsedData] something"
	lineSessionReset = "[ResetCurrentSession] something"
	lineWorldJoin    = "[Behaviour] Joining wrld_1af53798-92a3-4c3f-99ae-a7c42ec6084d"
	lineWorldLeave   = "[OnPlayerLeft] ローカルプレイヤーが Leave した"
	lineAppQuit      = "VRCApplication: HandleApplicationQuit"
	lineIrrelevant   = "some unrelated log line"
)

func savedataLine(userID, sig string, creditAll int64) string {
	data := fmt.Sprintf(`{"version":1,"lastsave":1700000000,"credit_all":%d}`, creditAll)
	params := url.Values{}
	params.Set("data", data)
	params.Set("user_id", userID)
	params.Set("sig", sig)
	return "https://example.com/api/v1/data?" + params.Encode()
}

func newHandler(w *mockWriter, a *mockSaveManager, enableAutosave bool) watcher.LineHandler {
	return NewHandler("test.log", w, a, enableAutosave)
}

func feed(h watcher.LineHandler, lines ...string) {
	for _, l := range lines {
		h("test.log", l)
	}
}

// --- テストケース ---

// 無関係な行はInfluxへの書き込みもautosaveも呼ばれない。
func TestHandler_IrrelevantLine_NoOp(t *testing.T) {
	w := &mockWriter{}
	a := &mockSaveManager{returnVal: true}
	h := newHandler(w, a, true)

	feed(h, lineIrrelevant)

	if w.count != 0 {
		t.Errorf("expected 0 influx writes, got %d", w.count)
	}
	if len(a.calls) != 0 {
		t.Errorf("expected 0 autosave calls, got %d", len(a.calls))
	}
}

// savedata行を受け取るとInfluxに1件書き込まれる。
func TestHandler_SavedataUpdate_WritesInflux(t *testing.T) {
	w := &mockWriter{}
	a := &mockSaveManager{returnVal: true}
	h := newHandler(w, a, true)

	feed(h, savedataLine("u1", "sig1", 100))

	if w.count != 1 {
		t.Errorf("expected 1 influx write, got %d", w.count)
	}
}

// autosave有効時はUpdateが呼ばれ、通常時はignoreRateLimit=false。
func TestHandler_SavedataUpdate_AutosaveEnabled_CallsUpdate(t *testing.T) {
	w := &mockWriter{}
	a := &mockSaveManager{returnVal: true}
	h := newHandler(w, a, true)

	feed(h, savedataLine("u1", "sig1", 100))

	if len(a.calls) != 1 {
		t.Fatalf("expected 1 autosave call, got %d", len(a.calls))
	}
	if a.calls[0].ignoreRateLimit {
		t.Error("expected ignoreRateLimit=false for normal savedata")
	}
}

// autosave無効時はInfluxには書くがUpdateは呼ばれない。
func TestHandler_SavedataUpdate_AutosaveDisabled_NoAutosave(t *testing.T) {
	w := &mockWriter{}
	a := &mockSaveManager{returnVal: true}
	h := newHandler(w, a, false)

	feed(h, savedataLine("u1", "sig1", 100))

	if w.count != 1 {
		t.Errorf("expected 1 influx write, got %d", w.count)
	}
	if len(a.calls) != 0 {
		t.Errorf("expected 0 autosave calls, got %d", len(a.calls))
	}
}

// WorldLeave後の最初のsavedataはignoreRateLimit=trueで保存される（退出URLの即時送信）。
func TestHandler_WorldLeave_NextSavedataIgnoresRateLimit(t *testing.T) {
	w := &mockWriter{}
	a := &mockSaveManager{returnVal: true}
	h := newHandler(w, a, true)

	feed(h, lineWorldLeave, savedataLine("u1", "sig1", 100))

	if len(a.calls) != 1 {
		t.Fatalf("expected 1 autosave call, got %d", len(a.calls))
	}
	if !a.calls[0].ignoreRateLimit {
		t.Error("expected ignoreRateLimit=true after WorldLeave")
	}
}

// WorldLeaveフラグは1回目のsavedataで解除され、2回目以降は通常のレート制限に戻る。
func TestHandler_WorldLeave_FlagClearedAfterFirstSavedata(t *testing.T) {
	w := &mockWriter{}
	a := &mockSaveManager{returnVal: true}
	h := newHandler(w, a, true)

	feed(h,
		lineWorldLeave,
		savedataLine("u1", "sig1", 100), // ignoreRateLimit=true
		savedataLine("u1", "sig2", 200), // ignoreRateLimit=false に戻る
	)

	if len(a.calls) != 2 {
		t.Fatalf("expected 2 autosave calls, got %d", len(a.calls))
	}
	if !a.calls[0].ignoreRateLimit {
		t.Error("first call: expected ignoreRateLimit=true")
	}
	if a.calls[1].ignoreRateLimit {
		t.Error("second call: expected ignoreRateLimit=false")
	}
}

// Updateが失敗して未保存状態のままAppQuitすると、再度ignoreRateLimit=trueでUpdateが呼ばれる。
func TestHandler_AppQuit_WithUnsavedRecord_CallsUpdate(t *testing.T) {
	w := &mockWriter{}
	a := &mockSaveManager{returnVal: false} // Update失敗 → hasUnsavedRecord=true になる
	h := newHandler(w, a, true)

	feed(h, savedataLine("u1", "sig1", 100), lineAppQuit)

	// 1回目: savedata、2回目: quit時の保存
	if len(a.calls) != 2 {
		t.Fatalf("expected 2 autosave calls, got %d", len(a.calls))
	}
	if !a.calls[1].ignoreRateLimit {
		t.Error("AppQuit save: expected ignoreRateLimit=true")
	}
}

// 直前のsavedataが正常に保存済みであればAppQuitでUpdateは呼ばれない。
func TestHandler_AppQuit_RecordAlreadySaved_NoUpdate(t *testing.T) {
	w := &mockWriter{}
	a := &mockSaveManager{returnVal: true} // Update成功 → hasUnsavedRecord=false
	h := newHandler(w, a, true)

	feed(h, savedataLine("u1", "sig1", 100), lineAppQuit)

	// savedata の1回だけ
	if len(a.calls) != 1 {
		t.Errorf("expected 1 autosave call, got %d", len(a.calls))
	}
}

// savedataを一度も受け取っていない状態でAppQuitしてもUpdateは呼ばれない。
func TestHandler_AppQuit_NoLastRecord_NoUpdate(t *testing.T) {
	w := &mockWriter{}
	a := &mockSaveManager{returnVal: true}
	h := newHandler(w, a, true)

	feed(h, lineAppQuit)

	if len(a.calls) != 0 {
		t.Errorf("expected 0 autosave calls, got %d", len(a.calls))
	}
}

// savedata以外のイベント行（CloudLoad/SessionReset/WorldJoin/WorldLeave/AppQuit）はInfluxもautosaveも呼ばれない。
func TestHandler_NonSavedataEvents_NoInfluxNoAutosave(t *testing.T) {
	lines := []string{lineCloudLoad, lineSessionReset, lineWorldJoin, lineWorldLeave, lineAppQuit}
	for _, line := range lines {
		w := &mockWriter{}
		a := &mockSaveManager{returnVal: true}
		h := newHandler(w, a, true)
		feed(h, line)
		if w.count != 0 {
			t.Errorf("line %q: expected 0 influx writes, got %d", line, w.count)
		}
	}
}
