package autosave

import (
	"errors"
	"testing"
	"time"

	"log-parser/internal/model"
)

// --- モック ---

type mockRepo struct {
	state     State
	found     bool
	getErr    error
	updateErr error
	updated   []State
}

func (r *mockRepo) Get(_ string) (State, bool, error) {
	return r.state, r.found, r.getErr
}

func (r *mockRepo) Update(_ string, s State) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.updated = append(r.updated, s)
	return nil
}

type mockDispatcher struct {
	enqueued  []string
	returnVal bool
}

func (d *mockDispatcher) Enqueue(userID, rawURL string) bool {
	d.enqueued = append(d.enqueued, rawURL)
	return d.returnVal
}

func (d *mockDispatcher) Close() {}

// --- ヘルパー ---

func newRecord(userID, rawURL, sig string, creditAll model.FlexInt) *model.MmpSaveRecord {
	return &model.MmpSaveRecord{
		UserID: userID,
		RawURL: rawURL,
		Sig:    sig,
		Data:   &model.MmpSaveData{CreditAll: creditAll},
	}
}

func newManager(repo Repository, d Dispatcher) *Manager {
	return NewManager(repo, time.Minute, d)
}

// --- テストケース ---

// 初回保存（既存レコードなし）では Enqueue が呼ばれ true を返す。
func TestUpdate_FirstTime_Enqueues(t *testing.T) {
	repo := &mockRepo{found: false}
	d := &mockDispatcher{returnVal: true}
	m := newManager(repo, d)

	rec := newRecord("u1", "http://example.com/save", "hash1", 100)
	got := m.TrySave(rec, false)

	if !got {
		t.Fatal("expected true on first save")
	}
	if len(d.enqueued) != 1 || d.enqueued[0] != rec.RawURL {
		t.Fatalf("expected 1 enqueue with correct URL, got %v", d.enqueued)
	}
	if len(repo.updated) != 1 {
		t.Fatal("expected repo.Update to be called once")
	}
}

// 直前に保存済みでレート制限内の場合、Enqueue せず false を返す。
func TestUpdate_RateLimit_Skips(t *testing.T) {
	repo := &mockRepo{
		found: true,
		state: State{
			CreditAll:     100,
			LastAttemptAt: time.Now(), // 直前に保存済み
			DataHash:      "old",
		},
	}
	d := &mockDispatcher{returnVal: true}
	m := newManager(repo, d)

	rec := newRecord("u1", "http://example.com/save", "new", 200)
	got := m.TrySave(rec, false)

	if got {
		t.Fatal("expected false due to rate limit")
	}
	if len(d.enqueued) != 0 {
		t.Fatal("expected no enqueue")
	}
}

// ignoreRateLimit=true の場合、レート制限内でも Enqueue して true を返す。
func TestUpdate_IgnoreRateLimit_Enqueues(t *testing.T) {
	repo := &mockRepo{
		found: true,
		state: State{
			CreditAll:     100,
			LastAttemptAt: time.Now(), // レート制限内
			DataHash:      "old",
		},
	}
	d := &mockDispatcher{returnVal: true}
	m := newManager(repo, d)

	rec := newRecord("u1", "http://example.com/save", "new", 200)
	got := m.TrySave(rec, true)

	if !got {
		t.Fatal("expected true when ignoreRateLimit=true")
	}
	if len(d.enqueued) != 1 {
		t.Fatal("expected 1 enqueue")
	}
}

// CreditAll が前回より減少している場合（ロールバック検出）、Enqueue せず false を返す。
func TestUpdate_Rollback_Skips(t *testing.T) {
	repo := &mockRepo{
		found: true,
		state: State{
			CreditAll:     500,
			LastAttemptAt: time.Now().Add(-time.Hour),
			DataHash:      "old",
		},
	}
	d := &mockDispatcher{returnVal: true}
	m := newManager(repo, d)

	rec := newRecord("u1", "http://example.com/save", "new", 100) // 減少
	got := m.TrySave(rec, false)

	if got {
		t.Fatal("expected false due to rollback detection")
	}
	if len(d.enqueued) != 0 {
		t.Fatal("expected no enqueue")
	}
}

// データハッシュが前回と同一（変化なし）の場合、Enqueue せず false を返す。
func TestUpdate_NoDataChange_Skips(t *testing.T) {
	repo := &mockRepo{
		found: true,
		state: State{
			CreditAll:     100,
			LastAttemptAt: time.Now().Add(-time.Hour),
			DataHash:      "same",
		},
	}
	d := &mockDispatcher{returnVal: true}
	m := newManager(repo, d)

	rec := newRecord("u1", "http://example.com/save", "same", 100)
	got := m.TrySave(rec, false)

	if got {
		t.Fatal("expected false due to no data change")
	}
	if len(d.enqueued) != 0 {
		t.Fatal("expected no enqueue")
	}
}

// リポジトリの Get がエラーを返した場合、Enqueue せず false を返す。
func TestUpdate_RepoGetError_ReturnsFalse(t *testing.T) {
	repo := &mockRepo{getErr: errors.New("db error")}
	d := &mockDispatcher{returnVal: true}
	m := newManager(repo, d)

	rec := newRecord("u1", "http://example.com/save", "hash1", 100)
	got := m.TrySave(rec, false)

	if got {
		t.Fatal("expected false on repo.Get error")
	}
	if len(d.enqueued) != 0 {
		t.Fatal("expected no enqueue")
	}
}

// リポジトリの Update がエラーを返した場合、Enqueue せず false を返す。
func TestUpdate_RepoUpdateError_ReturnsFalse(t *testing.T) {
	repo := &mockRepo{found: false, updateErr: errors.New("write error")}
	d := &mockDispatcher{returnVal: true}
	m := newManager(repo, d)

	rec := newRecord("u1", "http://example.com/save", "hash1", 100)
	got := m.TrySave(rec, false)

	if got {
		t.Fatal("expected false on repo.Update error")
	}
	if len(d.enqueued) != 0 {
		t.Fatal("expected no enqueue when repo.Update fails")
	}
}

// Enqueue がバッファ満杯などで false を返した場合、Update も false を返す。
func TestUpdate_EnqueueFails_ReturnsFalse(t *testing.T) {
	repo := &mockRepo{found: false}
	d := &mockDispatcher{returnVal: false} // バッファ満杯など
	m := newManager(repo, d)

	rec := newRecord("u1", "http://example.com/save", "hash1", 100)
	got := m.TrySave(rec, false)

	if got {
		t.Fatal("expected false when Enqueue returns false")
	}
}

// 保存成功時にリポジトリへ書き込む状態（CreditAll・DataHash・LastAttemptAt）が正しいことを検証する。
func TestUpdate_PersistsCorrectState(t *testing.T) {
	repo := &mockRepo{found: false}
	d := &mockDispatcher{returnVal: true}
	m := newManager(repo, d)

	rec := newRecord("u1", "http://example.com/save", "hash1", 300)
	before := time.Now()
	m.TrySave(rec, false)
	after := time.Now()

	if len(repo.updated) != 1 {
		t.Fatal("expected one state update")
	}
	s := repo.updated[0]
	if s.CreditAll != 300 {
		t.Errorf("CreditAll: got %d, want 300", s.CreditAll)
	}
	if s.DataHash != "hash1" {
		t.Errorf("DataHash: got %s, want hash1", s.DataHash)
	}
	if s.LastAttemptAt.Before(before) || s.LastAttemptAt.After(after) {
		t.Errorf("LastAttemptAt out of expected range: %v", s.LastAttemptAt)
	}
}
