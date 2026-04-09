package main_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/influxdata/influxdb-client-go/v2/api/write"

	"log-parser/internal/autosave"
	"log-parser/internal/cloudsave"
	"log-parser/internal/handler"
	"log-parser/internal/offset"
	"log-parser/internal/watcher"
)

// --- mocks ---

// mockInfluxWriter は WritePoint を記録するだけのInfluxモック。
type mockInfluxWriter struct {
	mu     sync.Mutex
	points []*write.Point
}

func (m *mockInfluxWriter) WritePoint(p *write.Point) {
	m.mu.Lock()
	m.points = append(m.points, p)
	m.mu.Unlock()
}

func (m *mockInfluxWriter) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.points)
}

// mockSender は Send の呼び出しを記録するautosaveモック。
// 実際のHTTPリクエストは行わない。
type mockSender struct {
	mu    sync.Mutex
	calls []string // rawURL
}

func (s *mockSender) Send(_ context.Context, rawURL, _ string) autosave.RequestResult {
	s.mu.Lock()
	s.calls = append(s.calls, rawURL)
	s.mu.Unlock()
	return autosave.ResultOK
}

func (s *mockSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// --- helpers ---

func savedataLine(userID, sig string, creditAll int64) string {
	data := fmt.Sprintf(`{"version":1,"lastsave":1700000000,"credit_all":%d}`, creditAll)
	params := url.Values{}
	params.Set("data", data)
	params.Set("user_id", userID)
	params.Set("sig", sig)
	return "https://example.com/api/v3/data?" + params.Encode()
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

// testStack は結合テスト用のコンポーネント一式を保持する。
type testStack struct {
	logDir  string
	dataDir string
	influx  *mockInfluxWriter
	sender  *mockSender
}

// newTestStack はテスト用のスタックを組み立てる。
func newTestStack(t *testing.T) *testStack {
	t.Helper()
	return &testStack{
		logDir:  t.TempDir(),
		dataDir: t.TempDir(),
		influx:  &mockInfluxWriter{},
		sender:  &mockSender{},
	}
}

// buildWatcher は testStack の設定でコンポーネントを結線して LogWatcher を返す。
// autoSaveMgr の Close は t.Cleanup に登録される。
func (s *testStack) buildWatcher(t *testing.T, enableAutosave bool) *watcher.LogWatcher {
	t.Helper()

	cloudRepo := cloudsave.NewJSONRepository(filepath.Join(s.dataDir, "cloudsave.json"))
	queue := autosave.NewSaveDispatcher(s.sender)
	autoSaveMgr := autosave.NewManager(cloudRepo, 0, queue)
	t.Cleanup(autoSaveMgr.Close)

	newHandlerFn := func(path string) watcher.LineHandler {
		return handler.NewHandler(filepath.Base(path), s.influx, autoSaveMgr, enableAutosave)
	}

	offsetRepo := offset.NewJSONRepository(filepath.Join(s.dataDir, "offsets.json"))
	return watcher.NewLogWatcher(s.logDir, newHandlerFn, offsetRepo, false).
		WithIntervals(10*time.Millisecond, 20*time.Millisecond, time.Hour, 100*time.Millisecond)
}

// runWatcher は LogWatcher をバックグラウンドで起動し、cancelとdoneチャネルを返す。
func runWatcher(w *watcher.LogWatcher) (cancel context.CancelFunc, done <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		_ = w.Run(ctx)
	}()
	return cancel, ch
}

// stopWatcher はウォッチャーをキャンセルしてシャットダウンを待つ。
func stopWatcher(t *testing.T, cancel context.CancelFunc, done <-chan struct{}) {
	t.Helper()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher did not shut down within timeout")
	}
}

// --- テストケース ---

// TestIntegration_SavedataLine_TriggersInfluxAndAutosave は
// savedata URLを含む行がInflux書き込みとautosave送信を両方発火することを検証する。
func TestIntegration_SavedataLine_TriggersInfluxAndAutosave(t *testing.T) {
	s := newTestStack(t)
	w := s.buildWatcher(t, true)

	cancel, done := runWatcher(w)
	defer stopWatcher(t, cancel, done)

	logPath := filepath.Join(s.logDir, "output_log_2024_01_01_00_00_00.txt")
	line := savedataLine("user1", "sig1", 1000)
	if err := os.WriteFile(logPath, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	waitFor(t, 3*time.Second, func() bool { return s.influx.count() >= 1 })
	waitFor(t, 3*time.Second, func() bool { return s.sender.count() >= 1 })

	if n := s.influx.count(); n != 1 {
		t.Errorf("influx: got %d writes, want 1", n)
	}
	if n := s.sender.count(); n != 1 {
		t.Errorf("autosave sender: got %d calls, want 1", n)
	}
}

// TestIntegration_AutosaveDisabled_SenderNotCalled は
// enableAutosave=false のとき Influx書き込みは行われるが
// autosave Sender は呼ばれないことを検証する。
func TestIntegration_AutosaveDisabled_SenderNotCalled(t *testing.T) {
	s := newTestStack(t)
	w := s.buildWatcher(t, false)

	cancel, done := runWatcher(w)
	defer stopWatcher(t, cancel, done)

	logPath := filepath.Join(s.logDir, "output_log_2024_01_01_00_00_00.txt")
	line := savedataLine("user1", "sig1", 1000)
	if err := os.WriteFile(logPath, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	waitFor(t, 3*time.Second, func() bool { return s.influx.count() >= 1 })
	time.Sleep(50 * time.Millisecond) // senderが誤って呼ばれていないか確認

	if n := s.influx.count(); n != 1 {
		t.Errorf("influx: got %d writes, want 1", n)
	}
	if n := s.sender.count(); n != 0 {
		t.Errorf("autosave sender: got %d calls, want 0", n)
	}
}

// TestIntegration_OffsetPersistedOnShutdown は
// シャットダウン時に処理済みバイト数が offsets.json へ保存されることを検証する。
func TestIntegration_OffsetPersistedOnShutdown(t *testing.T) {
	s := newTestStack(t)
	w := s.buildWatcher(t, false)

	logPath := filepath.Join(s.logDir, "output_log_test.txt")
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cancel, done := runWatcher(w)
	time.Sleep(150 * time.Millisecond) // 行を読み込む時間を与える
	stopWatcher(t, cancel, done)

	offsetsPath := filepath.Join(s.dataDir, "offsets.json")
	data, err := os.ReadFile(offsetsPath)
	if err != nil {
		t.Fatalf("offsets.json not found: %v", err)
	}

	var offsets map[string]int64
	if err := json.Unmarshal(data, &offsets); err != nil {
		t.Fatalf("invalid offsets JSON: %v", err)
	}

	want := int64(len(content))
	if got := offsets[logPath]; got != want {
		t.Errorf("offset: got %d, want %d", got, want)
	}
}

// TestIntegration_ResumeFromOffset は
// 再起動後に保存済みoffsetから再開し、処理済み行をスキップすることを検証する。
func TestIntegration_ResumeFromOffset(t *testing.T) {
	s := newTestStack(t)

	logPath := filepath.Join(s.logDir, "output_log_test.txt")

	// --- 1回目: 1行を処理してシャットダウン ---
	firstLine := savedataLine("user1", "sig1", 100) + "\n"
	if err := os.WriteFile(logPath, []byte(firstLine), 0o644); err != nil {
		t.Fatal(err)
	}

	w1 := s.buildWatcher(t, false)
	cancel1, done1 := runWatcher(w1)
	waitFor(t, 3*time.Second, func() bool { return s.influx.count() >= 1 })
	stopWatcher(t, cancel1, done1)

	if s.influx.count() != 1 {
		t.Fatalf("1st run: expected 1 influx write, got %d", s.influx.count())
	}

	// --- 2回目: 同じdataDirを使い新しい行を追記してから起動 ---
	secondLine := savedataLine("user1", "sig2", 200) + "\n"
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(secondLine); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	// influxとsenderをリセットして「2回目だけ」何件処理したか計測する
	s.influx = &mockInfluxWriter{}
	s.sender = &mockSender{}

	w2 := s.buildWatcher(t, false)
	cancel2, done2 := runWatcher(w2)
	defer stopWatcher(t, cancel2, done2)

	waitFor(t, 3*time.Second, func() bool { return s.influx.count() >= 1 })
	time.Sleep(50 * time.Millisecond) // sig1 が再処理されていないか確認

	if n := s.influx.count(); n != 1 {
		t.Errorf("2nd run: expected 1 influx write (new line only), got %d", n)
	}
}

// TestIntegration_MultipleFiles_EachHandledIndependently は
// 複数のログファイルがそれぞれ独立して処理されることを検証する。
func TestIntegration_MultipleFiles_EachHandledIndependently(t *testing.T) {
	s := newTestStack(t)
	w := s.buildWatcher(t, false)

	cancel, done := runWatcher(w)
	defer stopWatcher(t, cancel, done)

	for i, userID := range []string{"userA", "userB", "userC"} {
		logPath := filepath.Join(s.logDir, fmt.Sprintf("output_log_00%d.txt", i))
		line := savedataLine(userID, fmt.Sprintf("sig%d", i), int64(100*(i+1)))
		if err := os.WriteFile(logPath, []byte(line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	waitFor(t, 3*time.Second, func() bool { return s.influx.count() >= 3 })

	if n := s.influx.count(); n != 3 {
		t.Errorf("expected 3 influx writes (one per file), got %d", n)
	}
}

// TestIntegration_NewLineAppended_PickedUpWithoutRestart は
// 実行中のウォッチャーがファイルへの追記を自動的に検出することを検証する。
func TestIntegration_NewLineAppended_PickedUpWithoutRestart(t *testing.T) {
	s := newTestStack(t)
	w := s.buildWatcher(t, false)

	cancel, done := runWatcher(w)
	defer stopWatcher(t, cancel, done)

	logPath := filepath.Join(s.logDir, "output_log_test.txt")
	firstLine := savedataLine("user1", "sig1", 100) + "\n"
	if err := os.WriteFile(logPath, []byte(firstLine), 0o644); err != nil {
		t.Fatal(err)
	}

	waitFor(t, 3*time.Second, func() bool { return s.influx.count() >= 1 })

	// 実行中に追記
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	secondLine := savedataLine("user1", "sig2", 200) + "\n"
	if _, err := f.WriteString(secondLine); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	waitFor(t, 3*time.Second, func() bool { return s.influx.count() >= 2 })

	if n := s.influx.count(); n != 2 {
		t.Errorf("expected 2 influx writes, got %d", n)
	}
}

// TestIntegration_WorldLeave_NextSavedataBypassesRateLimit は
// WorldLeave後の最初のsavedataが即座に送信されることを検証する（end-to-endシナリオ）。
func TestIntegration_WorldLeave_NextSavedataBypassesRateLimit(t *testing.T) {
	s := newTestStack(t)

	// レート制限を長く設定（デフォルトの1800sに相当）しても、WorldLeave後は送信される
	cloudRepo := cloudsave.NewJSONRepository(filepath.Join(s.dataDir, "cloudsave.json"))
	queue := autosave.NewSaveDispatcher(s.sender)
	autoSaveMgr := autosave.NewManager(cloudRepo, 1800*time.Second, queue)
	t.Cleanup(autoSaveMgr.Close)

	newHandlerFn := func(path string) watcher.LineHandler {
		return handler.NewHandler(filepath.Base(path), s.influx, autoSaveMgr, true)
	}
	offsetRepo := offset.NewJSONRepository(filepath.Join(s.dataDir, "offsets.json"))
	w := watcher.NewLogWatcher(s.logDir, newHandlerFn, offsetRepo, false).
		WithIntervals(10*time.Millisecond, 20*time.Millisecond, time.Hour, 100*time.Millisecond)

	cancel, done := runWatcher(w)
	defer stopWatcher(t, cancel, done)

	// WorldLeave → savedata の順で書き込む
	worldLeaveLine := "[OnPlayerLeft] ローカルプレイヤーが Leave した"
	saveLine := savedataLine("user1", "sig1", 1000)
	content := worldLeaveLine + "\n" + saveLine + "\n"

	logPath := filepath.Join(s.logDir, "output_log_test.txt")
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// WorldLeave後の最初のsavedataはレート制限をバイパスして送信される
	waitFor(t, 3*time.Second, func() bool { return s.sender.count() >= 1 })

	if n := s.sender.count(); n != 1 {
		t.Errorf("expected 1 autosave send (WorldLeave bypass), got %d", n)
	}
}

// TestIntegration_SavedataRateLimit_SecondSavedataSkipped は
// レート制限内の2回目のsavedataがスキップされることを検証する。
func TestIntegration_SavedataRateLimit_SecondSavedataSkipped(t *testing.T) {
	s := newTestStack(t)

	cloudRepo := cloudsave.NewJSONRepository(filepath.Join(s.dataDir, "cloudsave.json"))
	queue := autosave.NewSaveDispatcher(s.sender)
	autoSaveMgr := autosave.NewManager(cloudRepo, 1800*time.Second, queue) // レート制限あり
	t.Cleanup(autoSaveMgr.Close)

	newHandlerFn := func(path string) watcher.LineHandler {
		return handler.NewHandler(filepath.Base(path), s.influx, autoSaveMgr, true)
	}
	offsetRepo := offset.NewJSONRepository(filepath.Join(s.dataDir, "offsets.json"))
	w := watcher.NewLogWatcher(s.logDir, newHandlerFn, offsetRepo, false).
		WithIntervals(10*time.Millisecond, 20*time.Millisecond, time.Hour, 100*time.Millisecond)

	cancel, done := runWatcher(w)
	defer stopWatcher(t, cancel, done)

	logPath := filepath.Join(s.logDir, "output_log_test.txt")
	line1 := savedataLine("user1", "sig1", 100) + "\n"
	line2 := savedataLine("user1", "sig2", 200) + "\n"
	if err := os.WriteFile(logPath, []byte(line1+line2), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1件目は送信され、2件目はレート制限でスキップされる
	waitFor(t, 3*time.Second, func() bool { return s.influx.count() >= 2 })
	time.Sleep(50 * time.Millisecond)

	if n := s.influx.count(); n != 2 {
		t.Errorf("influx: expected 2 writes, got %d", n)
	}
	if n := s.sender.count(); n != 1 {
		t.Errorf("autosave sender: expected 1 call (2nd skipped by rate limit), got %d", n)
	}
}

// TestIntegration_AppQuit_WithUnsavedRecord_ForcesAutosave は
// レート制限内の2回目のsavedataが未保存状態になった後、
// AppQuit行により強制送信されることを検証する（結線確認: 複数行の状態追跡 + 強制送信）。
func TestIntegration_AppQuit_WithUnsavedRecord_ForcesAutosave(t *testing.T) {
	s := newTestStack(t)

	cloudRepo := cloudsave.NewJSONRepository(filepath.Join(s.dataDir, "cloudsave.json"))
	queue := autosave.NewSaveDispatcher(s.sender)
	autoSaveMgr := autosave.NewManager(cloudRepo, 1800*time.Second, queue)
	t.Cleanup(autoSaveMgr.Close)

	newHandlerFn := func(path string) watcher.LineHandler {
		return handler.NewHandler(filepath.Base(path), s.influx, autoSaveMgr, true)
	}
	offsetRepo := offset.NewJSONRepository(filepath.Join(s.dataDir, "offsets.json"))
	w := watcher.NewLogWatcher(s.logDir, newHandlerFn, offsetRepo, false).
		WithIntervals(10*time.Millisecond, 20*time.Millisecond, time.Hour, 100*time.Millisecond)

	cancel, done := runWatcher(w)
	defer stopWatcher(t, cancel, done)

	// 1行目: 初回送信 → sender=1, hasUnsavedRecord=false
	// 2行目: レート制限でスキップ → sender=1, hasUnsavedRecord=true
	// 3行目: AppQuit → 強制送信 → sender=2
	line1 := savedataLine("user1", "sig1", 100) + "\n"
	line2 := savedataLine("user1", "sig2", 200) + "\n"
	appQuit := "VRCApplication: HandleApplicationQuit\n"
	content := line1 + line2 + appQuit

	logPath := filepath.Join(s.logDir, "output_log_test.txt")
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	waitFor(t, 3*time.Second, func() bool { return s.sender.count() >= 2 })

	if n := s.influx.count(); n != 2 {
		t.Errorf("influx: expected 2 writes, got %d", n)
	}
	if n := s.sender.count(); n != 2 {
		t.Errorf("sender: expected 2 calls (initial + AppQuit force), got %d", n)
	}
}

// TestIntegration_Rollback_SenderNotCalled は
// credit_allが減少した2回目のsavedataがロールバック検出によりスキップされることを検証する
// （結線確認: URLからcredit_allが正しく抽出されManagerまで伝播するか）。
func TestIntegration_Rollback_SenderNotCalled(t *testing.T) {
	s := newTestStack(t)
	w := s.buildWatcher(t, true) // レート制限なし（saveInterval=0）

	cancel, done := runWatcher(w)
	defer stopWatcher(t, cancel, done)

	line1 := savedataLine("user1", "sig1", 500) + "\n"
	line2 := savedataLine("user1", "sig2", 100) + "\n" // credit_all 減少 → ロールバック

	logPath := filepath.Join(s.logDir, "output_log_test.txt")
	if err := os.WriteFile(logPath, []byte(line1+line2), 0o644); err != nil {
		t.Fatal(err)
	}

	waitFor(t, 3*time.Second, func() bool { return s.influx.count() >= 2 })
	time.Sleep(50 * time.Millisecond)

	if n := s.influx.count(); n != 2 {
		t.Errorf("influx: expected 2 writes, got %d", n)
	}
	if n := s.sender.count(); n != 1 {
		t.Errorf("sender: expected 1 call (rollback skipped), got %d", n)
	}
}

// TestIntegration_SameSig_SenderNotCalled は
// 同一sigの2回目のsavedataがデータ変更なしとしてスキップされることを検証する
// （結線確認: URLからsigが正しく抽出されManagerまで伝播するか）。
func TestIntegration_SameSig_SenderNotCalled(t *testing.T) {
	s := newTestStack(t)
	w := s.buildWatcher(t, true) // レート制限なし（saveInterval=0）

	cancel, done := runWatcher(w)
	defer stopWatcher(t, cancel, done)

	line1 := savedataLine("user1", "sig1", 100) + "\n"
	line2 := savedataLine("user1", "sig1", 100) + "\n" // 同一sig・同一データ

	logPath := filepath.Join(s.logDir, "output_log_test.txt")
	if err := os.WriteFile(logPath, []byte(line1+line2), 0o644); err != nil {
		t.Fatal(err)
	}

	waitFor(t, 3*time.Second, func() bool { return s.influx.count() >= 2 })
	time.Sleep(50 * time.Millisecond)

	if n := s.influx.count(); n != 2 {
		t.Errorf("influx: expected 2 writes, got %d", n)
	}
	if n := s.sender.count(); n != 1 {
		t.Errorf("sender: expected 1 call (same-sig skipped), got %d", n)
	}
}
