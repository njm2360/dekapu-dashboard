package watcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- mock ---

type mockRepo struct {
	mu      sync.Mutex
	data    map[string]int64
	loadErr error
	saves   []map[string]int64
}

func (m *mockRepo) Load() (map[string]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	out := make(map[string]int64, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return out, nil
}

func (m *mockRepo) Save(offsets map[string]int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make(map[string]int64, len(offsets))
	for k, v := range offsets {
		cp[k] = v
	}
	m.saves = append(m.saves, cp)
	return nil
}

func (m *mockRepo) lastSave() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.saves) == 0 {
		return nil
	}
	return m.saves[len(m.saves)-1]
}

// --- helpers ---

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

// newTestWatcher は短いインターバルを設定した LogWatcher を返す。
func newTestWatcher(logDir string, repo *mockRepo, readFromEnd bool, newHandler func(path string) LineHandler) *LogWatcher {
	w := NewLogWatcher(logDir, newHandler, repo, readFromEnd)
	w.pollInterval = 10 * time.Millisecond
	w.scanInterval = 20 * time.Millisecond
	w.stateSaveInterval = time.Hour // テスト中に自動保存しない
	w.idleTimeout = 50 * time.Millisecond
	return w
}

// --- readLines tests ---

// 空ファイルは行なし・offset 0 を返す。
func TestReadLines_Empty(t *testing.T) {
	f, err := os.CreateTemp("", "watcher_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	lines, offset, err := readLines(f)
	f.Close()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("expected no lines, got %v", lines)
	}
	if offset != 0 {
		t.Fatalf("expected offset 0, got %d", offset)
	}
}

// 1 行のファイルを正しく読み取り、offset が行末まで進む。
func TestReadLines_SingleLine(t *testing.T) {
	f, err := os.CreateTemp("", "watcher_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("hello world\n")
	f.Seek(0, 0)

	lines, offset, err := readLines(f)
	f.Close()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 1 || lines[0] != "hello world" {
		t.Fatalf("got %v", lines)
	}
	want := int64(len("hello world\n"))
	if offset != want {
		t.Fatalf("expected offset %d, got %d", want, offset)
	}
}

// 複数行をすべて正しく読み取る。
func TestReadLines_MultipleLines(t *testing.T) {
	f, err := os.CreateTemp("", "watcher_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("line1\nline2\nline3\n")
	f.Seek(0, 0)

	lines, _, err := readLines(f)
	f.Close()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"line1", "line2", "line3"}
	if len(lines) != len(want) {
		t.Fatalf("got %v, want %v", lines, want)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Fatalf("lines[%d] = %q, want %q", i, lines[i], w)
		}
	}
}

// 末尾に改行がない行は完結していないため返さない。
func TestReadLines_PartialLine(t *testing.T) {
	f, err := os.CreateTemp("", "watcher_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("complete\nincomplete")
	f.Seek(0, 0)

	lines, offset, err := readLines(f)
	f.Close()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 1 || lines[0] != "complete" {
		t.Fatalf("got %v", lines)
	}
	// offset は完結した行の末尾まで
	if offset != int64(len("complete\n")) {
		t.Fatalf("expected offset %d, got %d", len("complete\n"), offset)
	}
}

// CRLF 改行の CR が取り除かれて返される。
func TestReadLines_CRLF(t *testing.T) {
	f, err := os.CreateTemp("", "watcher_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("line1\r\nline2\r\n")
	f.Seek(0, 0)

	lines, _, err := readLines(f)
	f.Close()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %v", lines)
	}
	for _, l := range lines {
		if strings.Contains(l, "\r") {
			t.Fatalf("CR not stripped: %q", l)
		}
	}
}

// 2回目の呼び出しで前回の続きから読める（offset が正しく進む）。
func TestReadLines_AdvancesOffset(t *testing.T) {
	f, err := os.CreateTemp("", "watcher_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("first\nsecond\n")
	f.Seek(0, 0)

	lines1, _, _ := readLines(f)
	lines2, _, _ := readLines(f)
	f.Close()

	if len(lines1) != 2 {
		t.Fatalf("first call: got %v", lines1)
	}
	// 2回目は同じ内容を再読みしない
	if len(lines2) != 0 {
		t.Fatalf("second call should return no lines, got %v", lines2)
	}
}

// --- NewLogWatcher tests ---

// 起動時に repo から offset を読み込んで保持する。
func TestNewLogWatcher_LoadsOffsets(t *testing.T) {
	// stale-pruning に消されないよう実在するファイルを作る
	f, err := os.CreateTemp("", "output_log_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	repo := &mockRepo{data: map[string]int64{f.Name(): 42}}
	w := NewLogWatcher(t.TempDir(), func(string) LineHandler { return nil }, repo, false)

	w.mu.Lock()
	got := w.offsets[f.Name()]
	w.mu.Unlock()

	if got != 42 {
		t.Fatalf("expected offset 42, got %d", got)
	}
}

// 存在しないファイルの offset は起動時に削除される。
func TestNewLogWatcher_PrunesStaleOffsets(t *testing.T) {
	repo := &mockRepo{data: map[string]int64{"/nonexistent/path/file.txt": 99}}
	w := NewLogWatcher(t.TempDir(), func(string) LineHandler { return nil }, repo, false)

	w.mu.Lock()
	_, ok := w.offsets["/nonexistent/path/file.txt"]
	w.mu.Unlock()

	if ok {
		t.Fatal("stale offset should have been removed")
	}
}

// repo の Load が失敗した場合は空の offset マップで起動する。
func TestNewLogWatcher_LoadError_UsesEmptyMap(t *testing.T) {
	repo := &mockRepo{loadErr: errors.New("disk read error")}
	w := NewLogWatcher(t.TempDir(), func(string) LineHandler { return nil }, repo, false)

	w.mu.Lock()
	n := len(w.offsets)
	w.mu.Unlock()

	if n != 0 {
		t.Fatalf("expected empty offsets on load error, got %d entries", n)
	}
}

// --- watchFile tests ---

// ファイルの各行がハンドラへ順番に渡される。
func TestWatchFile_DispatchesLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output_log_test.txt")
	if err := os.WriteFile(path, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := &mockRepo{data: make(map[string]int64)}

	var mu sync.Mutex
	var received []string
	handler := func(_, line string) {
		mu.Lock()
		received = append(received, line)
		mu.Unlock()
	}

	w := newTestWatcher(dir, repo, false, func(string) LineHandler { return handler })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.watchFile(ctx, path, handler)

	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) >= 2
	})
	cancel()

	mu.Lock()
	defer mu.Unlock()
	if len(received) < 2 || received[0] != "line1" || received[1] != "line2" {
		t.Fatalf("got %v", received)
	}
}

// readFromEnd=true かつ保存済み offset なし → 既存内容はスキップして新規追記分だけ受け取る。
func TestWatchFile_ReadsFromEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output_log_test.txt")
	if err := os.WriteFile(path, []byte("old line\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := &mockRepo{data: make(map[string]int64)}

	var mu sync.Mutex
	var received []string
	handler := func(_, line string) {
		mu.Lock()
		received = append(received, line)
		mu.Unlock()
	}

	w := newTestWatcher(dir, repo, true, func(string) LineHandler { return handler })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.watchFile(ctx, path, handler)

	// watchFile が EOF へシークするのを待ってから追記
	time.Sleep(30 * time.Millisecond)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("new line\n")
	f.Close()

	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) >= 1
	})
	cancel()

	mu.Lock()
	defer mu.Unlock()
	for _, l := range received {
		if l == "old line" {
			t.Fatal("old line should be skipped when readFromEnd=true")
		}
	}
	if len(received) == 0 || received[0] != "new line" {
		t.Fatalf("expected 'new line', got %v", received)
	}
}

// 保存済み offset から再開する。
func TestWatchFile_ReadsFromOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output_log_test.txt")
	content := "skip this\nread this\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	skipOffset := int64(len("skip this\n"))
	repo := &mockRepo{data: map[string]int64{path: skipOffset}}

	var mu sync.Mutex
	var received []string
	handler := func(_, line string) {
		mu.Lock()
		received = append(received, line)
		mu.Unlock()
	}

	w := newTestWatcher(dir, repo, false, func(string) LineHandler { return handler })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.watchFile(ctx, path, handler)

	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) >= 1
	})
	cancel()

	mu.Lock()
	defer mu.Unlock()
	for _, l := range received {
		if l == "skip this" {
			t.Fatal("line before offset should be skipped")
		}
	}
	if len(received) == 0 || received[0] != "read this" {
		t.Fatalf("expected 'read this', got %v", received)
	}
}

// 一定時間新規行がなければ watchFile が自律終了する。
func TestWatchFile_IdleTimeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output_log_test.txt")
	if err := os.WriteFile(path, []byte("line\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := &mockRepo{data: make(map[string]int64)}
	w := newTestWatcher(dir, repo, false, func(string) LineHandler { return nil })
	w.idleTimeout = 30 * time.Millisecond

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.watchFile(context.Background(), path, func(_, _ string) {})
	}()

	select {
	case <-done:
		// idle timeout により正常終了
	case <-time.After(2 * time.Second):
		t.Fatal("watchFile did not exit after idle timeout")
	}
}

// --- doScan tests ---

// 未監視のファイルを発見すると watchFile を起動して行を受け取る。
func TestDoScan_StartsNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output_log_001.txt")
	if err := os.WriteFile(path, []byte("data\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := &mockRepo{data: make(map[string]int64)}

	var mu sync.Mutex
	var received []string

	w := newTestWatcher(dir, repo, false, func(string) LineHandler {
		return func(_, line string) {
			mu.Lock()
			received = append(received, line)
			mu.Unlock()
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.doScan(ctx, true)

	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) >= 1
	})

	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 || received[0] != "data" {
		t.Fatalf("expected 'data', got %v", received)
	}
}

// offset がファイルサイズ以上の場合は watchFile を起動しない。
func TestDoScan_SkipsFullyConsumedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output_log_001.txt")
	content := "old data\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := &mockRepo{data: map[string]int64{path: int64(len(content))}}
	w := newTestWatcher(dir, repo, false, func(string) LineHandler { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.doScan(ctx, true)
	time.Sleep(30 * time.Millisecond)

	w.mu.Lock()
	_, active := w.cancelFns[path]
	known := w.knownFiles[path]
	w.mu.Unlock()

	if active {
		t.Fatal("watchFile should not be started for a fully consumed file")
	}
	if !known {
		t.Fatal("file should be marked as known")
	}
}

// known かつ inactive なファイルに新規内容があれば監視を再開する。
func TestDoScan_ResumesKnownFileWithNewContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output_log_001.txt")
	existing := "existing\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := &mockRepo{data: map[string]int64{path: int64(len(existing))}}

	var mu sync.Mutex
	var received []string

	w := newTestWatcher(dir, repo, false, func(string) LineHandler {
		return func(_, line string) {
			mu.Lock()
			received = append(received, line)
			mu.Unlock()
		}
	})

	// known だが active ではない状態を作る
	w.mu.Lock()
	w.knownFiles[path] = true
	w.mu.Unlock()

	// ファイルに新規行を追記
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("new content\n")
	f.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.doScan(ctx, false)

	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) >= 1
	})

	mu.Lock()
	defer mu.Unlock()
	if len(received) == 0 || received[0] != "new content" {
		t.Fatalf("expected 'new content', got %v", received)
	}
}

// --- Run tests ---

// Run が終了時に offset を保存する。
func TestRun_SavesOffsetsOnShutdown(t *testing.T) {
	dir := t.TempDir()
	repo := &mockRepo{data: make(map[string]int64)}
	w := newTestWatcher(dir, repo, false, func(string) LineHandler { return nil })

	w.mu.Lock()
	w.offsets["some/file.txt"] = 123
	w.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not complete after context cancel")
	}

	last := repo.lastSave()
	if last == nil {
		t.Fatal("expected at least one Save call")
	}
	if last["some/file.txt"] != 123 {
		t.Fatalf("expected offset 123 in final save, got %v", last)
	}
}
