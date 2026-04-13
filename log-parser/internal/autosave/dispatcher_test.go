package autosave

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockSender はテスト用の Sender 実装。
// calls に呼び出し引数を記録し、results を順に返す（末尾は繰り返す）。
type mockSender struct {
	mu      sync.Mutex
	results []RequestResult
	calls   []sendCall
}

type sendCall struct {
	rawURL string
	userID string
}

func (m *mockSender) Send(_ context.Context, rawURL, userID string) RequestResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, sendCall{rawURL: rawURL, userID: userID})
	idx := len(m.calls) - 1
	if idx >= len(m.results) {
		idx = len(m.results) - 1
	}
	return m.results[idx]
}

func (m *mockSender) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// newTestQueue は backoff を極小にした SaveDispatcher を返す（テストが遅くならないよう）。
func newTestQueue(s Sender) *SaveDispatcher {
	q := NewSaveDispatcher(s)
	q.backoffBase = time.Millisecond
	q.backoffMax = 10 * time.Millisecond
	return q
}

// waitFor はポーリングで条件が true になるまで最大 timeout 待つ。
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

// --- テストケース ---

// キューがオープン中は Enqueue が true を返す。
func TestEnqueue_ReturnsTrueWhenOpen(t *testing.T) {
	s := &mockSender{results: []RequestResult{ResultOK}}
	q := newTestQueue(s)
	defer q.Close()

	ok := q.Enqueue("u1", "http://example.com")
	if !ok {
		t.Fatal("Enqueue should return true on open queue")
	}
}

// Close 後は Enqueue が false を返す。
func TestEnqueue_ReturnsFalseAfterClose(t *testing.T) {
	s := &mockSender{results: []RequestResult{ResultOK}}
	q := newTestQueue(s)
	q.Close()

	ok := q.Enqueue("u1", "http://example.com")
	if ok {
		t.Fatal("Enqueue should return false after Close")
	}
}

// ResultOK が返った場合、リトライなしで1回だけ Send が呼ばれる。
func TestSend_SuccessNoRetry(t *testing.T) {
	s := &mockSender{results: []RequestResult{ResultOK}}
	q := newTestQueue(s)

	q.Enqueue("u1", "http://example.com/save")
	waitFor(t, time.Second, func() bool { return s.callCount() >= 1 })
	q.Close()

	if s.callCount() != 1 {
		t.Fatalf("expected 1 Send call, got %d", s.callCount())
	}
	if s.calls[0].rawURL != "http://example.com/save" || s.calls[0].userID != "u1" {
		t.Fatalf("unexpected call args: %+v", s.calls[0])
	}
}

// ResultPermanent が返った場合、リトライせず1回で終わる。
func TestSend_PermanentFailureNoRetry(t *testing.T) {
	s := &mockSender{results: []RequestResult{ResultPermanent}}
	q := newTestQueue(s)

	q.Enqueue("u1", "http://example.com/save")
	waitFor(t, time.Second, func() bool { return s.callCount() >= 1 })
	q.Close()

	if s.callCount() != 1 {
		t.Fatalf("expected 1 Send call (no retry on permanent), got %d", s.callCount())
	}
}

// ResultRetryable が返り続けた場合、maxRetries 回リトライして終了する。
func TestSend_RetryableExhaustsMaxRetries(t *testing.T) {
	s := &mockSender{results: []RequestResult{ResultRetryable}}
	q := newTestQueue(s)

	q.Enqueue("u1", "http://example.com/save")

	// すべてのリトライが終わるまで待ってから Close
	want := maxRetries + 1
	waitFor(t, 5*time.Second, func() bool { return s.callCount() >= want })
	q.Close()

	if s.callCount() != want {
		t.Fatalf("expected %d Send calls, got %d", want, s.callCount())
	}
}

// 1回目が ResultRetryable で2回目が ResultOK の場合、計2回 Send が呼ばれる。
func TestSend_RetrySucceedsOnSecondAttempt(t *testing.T) {
	s := &mockSender{results: []RequestResult{ResultRetryable, ResultOK}}
	q := newTestQueue(s)

	q.Enqueue("u1", "http://example.com/save")

	// 2 回目の Send（成功）が終わるまで待ってから Close
	waitFor(t, 5*time.Second, func() bool { return s.callCount() >= 2 })
	q.Close()

	if s.callCount() != 2 {
		t.Fatalf("expected 2 Send calls, got %d", s.callCount())
	}
}


// 同一ユーザーへの Enqueue はエンキュー順に処理される。
func TestPerUserOrdering(t *testing.T) {
	var mu sync.Mutex
	var order []string

	s := &funcSender{fn: func(rawURL, userID string) RequestResult {
		mu.Lock()
		order = append(order, rawURL)
		mu.Unlock()
		return ResultOK
	}}

	q := newTestQueue(s)
	for i := 0; i < 5; i++ {
		url := "http://example.com/" + string(rune('a'+i))
		q.Enqueue("u1", url)
	}
	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(order) >= 5
	})
	q.Close()

	mu.Lock()
	defer mu.Unlock()
	for i, got := range order {
		want := "http://example.com/" + string(rune('a'+i))
		if got != want {
			t.Fatalf("order[%d]: got %s, want %s", i, got, want)
		}
	}
}

// 異なるユーザーはそれぞれ独立したワーカーを持ち、並列処理される。
func TestMultipleUsers_IndependentWorkers(t *testing.T) {
	var count atomic.Int32
	s := &funcSender{fn: func(rawURL, userID string) RequestResult {
		count.Add(1)
		return ResultOK
	}}

	q := newTestQueue(s)
	q.Enqueue("u1", "http://example.com/a")
	q.Enqueue("u2", "http://example.com/b")
	q.Enqueue("u3", "http://example.com/c")
	waitFor(t, time.Second, func() bool { return count.Load() >= 3 })
	q.Close()

	if int(count.Load()) != 3 {
		t.Fatalf("expected 3 Send calls, got %d", count.Load())
	}

	// 3 つの別ユーザーはそれぞれ独立したワーカーを持つ
	q2 := newTestQueue(s)
	q2.Enqueue("u1", "")
	q2.Enqueue("u1", "")
	q2.Enqueue("u2", "")
	q2.Close()

	q2.mu.Lock()
	workerCount := len(q2.workers)
	q2.mu.Unlock()
	if workerCount != 2 {
		t.Fatalf("expected 2 workers (u1, u2), got %d", workerCount)
	}
}

// Close 呼び出しはバックオフ待機中のリトライを中断する。
func TestShutdownCancelsRetry(t *testing.T) {
	// 最初だけ retryable を返し、以降は OK を返す（が Close で止まることを確認）
	var calls atomic.Int32
	s := &funcSender{fn: func(rawURL, userID string) RequestResult {
		calls.Add(1)
		return ResultRetryable
	}}

	q := NewSaveDispatcher(s) // backoff はデフォルト (2s) — Close で中断されることを確認
	q.backoffBase = 500 * time.Millisecond
	q.backoffMax = 2 * time.Minute

	q.Enqueue("u1", "http://example.com/save")

	// 1 回目の Send が終わったら Close（バックオフ中に割り込む）
	waitFor(t, time.Second, func() bool { return calls.Load() >= 1 })
	q.Close()

	// バックオフが 500ms なので、Close 後すぐに完了していれば maxRetries 回未満
	if calls.Load() >= int32(maxRetries+1) {
		t.Fatalf("expected retry to be cancelled by Close, but got %d calls", calls.Load())
	}
}

// バッファ満杯かつ done 閉済の場合、Enqueue はブロックせず即 false を返す（第2の done チェック）。
func TestEnqueue_ReturnsFalseWhenDoneClosedWithFullBuffer(t *testing.T) {
	block := make(chan struct{})
	s := &funcSender{fn: func(rawURL, userID string) RequestResult {
		<-block
		return ResultOK
	}}

	q := newTestQueue(s)

	// ワーカーが1件目を拾ってSend内でブロックするのを待つ
	q.Enqueue("u1", "http://example.com/0")
	time.Sleep(5 * time.Millisecond)

	// バッファを満杯にする
	for i := 1; i <= workerBufSize; i++ {
		q.Enqueue("u1", fmt.Sprintf("http://example.com/%d", i))
	}

	// done を閉じる（wg.Wait は呼ばない）
	close(q.done)

	// バッファ満杯 + done 閉済 → 第2の case <-q.done で即 false を返すはず
	ok := q.Enqueue("u1", "http://example.com/overflow")
	if ok {
		t.Fatal("Enqueue should return false when done is closed and buffer is full")
	}

	// ワーカーをアンブロックして後始末
	close(block)
	q.wg.Wait()
}

// バッファ満杯時は Enqueue が即 false を返す。
func TestEnqueue_ReturnsFalseWhenBufferFull(t *testing.T) {
	block := make(chan struct{})
	s := &funcSender{fn: func(rawURL, userID string) RequestResult {
		<-block
		return ResultOK
	}}

	q := newTestQueue(s)

	// ワーカーが1件目を拾ってブロックするのを待つ
	q.Enqueue("u1", "http://example.com/0")
	time.Sleep(5 * time.Millisecond)

	// バッファを満杯にする
	for i := 1; i <= workerBufSize; i++ {
		q.Enqueue("u1", fmt.Sprintf("http://example.com/%d", i))
	}

	// バッファ満杯 → 即 false を返すはず
	ok := q.Enqueue("u1", "http://example.com/overflow")
	if ok {
		t.Fatal("Enqueue should return false when buffer is full")
	}

	close(block)
	q.Close()
}


// 複数ゴルーチンから同時に Enqueue してもデータ競合が起きないことを検証する（go test -race で有効）。
func TestConcurrentEnqueue_RaceSafe(t *testing.T) {
	const goroutines = 8
	const perGoroutine = 20

	var count atomic.Int32
	s := &funcSender{fn: func(rawURL, userID string) RequestResult {
		count.Add(1)
		return ResultOK
	}}

	q := newTestQueue(s)

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				q.Enqueue(fmt.Sprintf("u%d", id), fmt.Sprintf("http://example.com/%d/%d", id, i))
			}
		}(g)
	}
	wg.Wait()
	q.Close()

	// バッファ満杯時はドロップされるため全件処理は保証しない。
	// このテストの目的はデータ競合がないことの検証（go test -race）。
	if count.Load() == 0 {
		t.Fatal("expected at least one Send call")
	}
}

// funcSender は関数を Sender に変換するヘルパー。
type funcSender struct {
	fn func(rawURL, userID string) RequestResult
}

func (f *funcSender) Send(_ context.Context, rawURL, userID string) RequestResult {
	return f.fn(rawURL, userID)
}
