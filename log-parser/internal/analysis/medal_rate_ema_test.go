package analysis

import (
	"math"
	"testing"
	"time"
)

const tolerance = 0.5 // medals/min

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) <= tolerance
}

var base = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

func advance(d time.Duration) time.Time {
	return base.Add(d)
}

// --- Update ---

func TestUpdate_FirstCall_ReturnsNil(t *testing.T) {
	e := NewMedalRateEMA(20)
	got := e.Update(1000, base)
	if got != nil {
		t.Fatalf("expected nil on first call, got %d", *got)
	}
}

func TestUpdate_SecondCall_ReturnsRate(t *testing.T) {
	e := NewMedalRateEMA(20)
	e.Update(0, base)

	// 60秒で120メダル増加 → 瞬間レート 120/min
	got := e.Update(120, advance(60*time.Second))
	if got == nil {
		t.Fatal("expected non-nil on second call")
	}
	// alpha = 1 - exp(-60/20) ≈ 0.950
	// emaRate = 0.950 * 120 ≈ 114
	alpha := 1 - math.Exp(-60.0/20.0)
	want := alpha * 120.0
	if !approxEqual(float64(*got), want) {
		t.Errorf("got %d, want %.1f (±%.1f)", *got, want, tolerance)
	}
}

func TestUpdate_SteadyRate_ConvergesToInstantRate(t *testing.T) {
	// decayConst を極小にすると alpha≈1 となり EMA が瞬間レートに収束する
	e := NewMedalRateEMA(0.001)
	e.Update(0, base)

	got := e.Update(300, advance(60*time.Second)) // 300/min
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if !approxEqual(float64(*got), 300.0) {
		t.Errorf("got %d, want 300", *got)
	}
}

func TestUpdate_EMASmoothing_SlowerThanInstant(t *testing.T) {
	// decayConst=20 では EMA は瞬間レートより緩やかに変化する
	e := NewMedalRateEMA(20)
	e.Update(0, base)
	e.Update(60, advance(60*time.Second)) // 60/min で初期化

	// 突然レートが跳ね上がっても EMA はすぐには追いつかない
	got := e.Update(60+6000, advance(120*time.Second)) // 瞬間 6000/min
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if float64(*got) >= 6000 {
		t.Errorf("EMA should be lower than instant rate 6000, got %d", *got)
	}
}

func TestUpdate_ZeroDt_ReturnsPreviousRate(t *testing.T) {
	e := NewMedalRateEMA(20)
	e.Update(0, base)
	first := e.Update(120, advance(60*time.Second))
	if first == nil {
		t.Fatal("expected non-nil")
	}

	// 同一タイムスタンプ → dt=0 → 現在の EMA をそのまま返す
	same := e.Update(200, advance(60*time.Second))
	if same == nil {
		t.Fatal("expected non-nil on zero-dt call")
	}
	if *same != *first {
		t.Errorf("zero-dt: got %d, want %d", *same, *first)
	}
}

// --- AddOffset ---

func TestAddOffset_CompensatesOverflow(t *testing.T) {
	// シナリオ: 1000メダル保持 → ストック溢れ+500 → さらに120獲得 = total 1620
	// オフセット500を加算すると増分が溢れ分を除いた120/minになる
	e := NewMedalRateEMA(0.001) // alpha≈1: EMA ≈ 瞬間レート
	e.Update(1000, base)
	e.AddOffset(500)

	got := e.Update(1620, advance(60*time.Second))
	if got == nil {
		t.Fatal("expected non-nil")
	}
	// adjusted=1620-500=1120、delta=1120-1000=120 → 120/min
	if !approxEqual(float64(*got), 120.0) {
		t.Errorf("got %d, want 120 (offset should cancel overflow)", *got)
	}
}

func TestAddOffset_WithoutOffset_InflatesRate(t *testing.T) {
	// オフセットなしでは溢れ分がレートに混入する
	e := NewMedalRateEMA(0.001)
	e.Update(1000, base)
	// offset なし: delta=1620-1000=620 → 620/min
	got := e.Update(1620, advance(60*time.Second))
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if !approxEqual(float64(*got), 620.0) {
		t.Errorf("got %d, want 620 (no offset)", *got)
	}
}

// --- Reset ---

func TestReset_ClearsState(t *testing.T) {
	e := NewMedalRateEMA(20)
	e.Update(0, base)
	e.Update(120, advance(60*time.Second))

	e.Reset()

	// Reset 後は初回扱いになる
	got := e.Update(9999, advance(120*time.Second))
	if got != nil {
		t.Fatalf("expected nil after Reset, got %d", *got)
	}
}

func TestReset_ClearsOffset(t *testing.T) {
	e := NewMedalRateEMA(0.001)
	e.AddOffset(1000)
	e.Reset()

	e.Update(0, base)
	// offset がリセットされていれば adjusted=500、増分=500 → 500/min
	got := e.Update(500, advance(60*time.Second))
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if !approxEqual(float64(*got), 500.0) {
		t.Errorf("got %d, want 500 (offset should be cleared)", *got)
	}
}
