package autosave

import (
	"log"
	"time"

	"log-parser/internal/model"
)

type Manager struct {
	repo         Repository
	saveInterval time.Duration
	queue        Dispatcher
}

func NewManager(repo Repository, saveInterval time.Duration, queue Dispatcher) *Manager {
	return &Manager{
		repo:         repo,
		saveInterval: saveInterval,
		queue:        queue,
	}
}

func (m *Manager) Close() {
	m.queue.Close()
}

func (m *Manager) TrySave(record *model.MmpSaveRecord, ignoreRateLimit bool) bool {
	now := time.Now()
	userID := record.UserID
	dataHash := record.Sig
	creditAll := int64(record.Data.CreditAll)

	state, ok, err := m.repo.Get(userID)
	if err != nil {
		log.Printf("[AutoSave] Failed to read state for user=%s: %v", userID, err)
		return false
	}

	if ok {
		if !ignoreRateLimit && now.Sub(state.LastAttemptAt) < m.saveInterval {
			// log.Printf("[AutoSave] Save skipped (rate limit) user=%s", userID)
			return false
		}
		if creditAll < state.CreditAll {
			log.Printf("[AutoSave] Data rollback detected, skipping user=%s", userID)
			return false
		}
		if dataHash == state.DataHash {
			log.Printf("[AutoSave] Save skipped (no data change) user=%s", userID)
			return false
		}
	}

	if err := m.repo.Update(userID, State{
		CreditAll:     creditAll,
		LastAttemptAt: now,
		DataHash:      dataHash,
	}); err != nil {
		log.Printf("[AutoSave] Failed to persist state for user=%s: %v", userID, err)
		return false
	}

	return m.queue.Enqueue(userID, record.RawURL)
}
