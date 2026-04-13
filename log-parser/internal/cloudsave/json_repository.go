package cloudsave

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"log-parser/internal/autosave"
)

type jsonUserRecord struct {
	CreditAll     int64     `json:"creditAll"`
	LastAttemptAt time.Time `json:"lastAttemptAt"`
	DataHash      string    `json:"dataHash"`
}

type jsonCloudSaveFile struct {
	Version int                       `json:"version"`
	Users   map[string]jsonUserRecord `json:"users"`
}

const jsonCloudSaveVersion = 2

type JSONRepository struct {
	path string
	mu   sync.RWMutex
	data map[string]autosave.State
}

var _ autosave.Repository = (*JSONRepository)(nil)

func NewJSONRepository(path string) *JSONRepository {
	r := &JSONRepository{
		path: path,
		data: make(map[string]autosave.State),
	}
	if err := r.load(); err != nil {
		log.Printf("[CloudSaveRepo] Failed to load %s: %v", path, err)
	}
	return r
}

func (r *JSONRepository) Get(userID string) (autosave.State, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state, ok := r.data[userID]
	return state, ok, nil
}

func (r *JSONRepository) Update(userID string, state autosave.State) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[userID] = state
	return r.save()
}

func (r *JSONRepository) load() error {
	raw, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var probe struct {
		Version *int `json:"version"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	if probe.Version == nil {
		// v1: flat map of user_id → {creditAll, lastAttemptAt} (no dataHash)
		if err := r.migrateV1(raw); err != nil {
			return fmt.Errorf("v1 migration: %w", err)
		}
		log.Printf("[CloudSaveRepo] Migrated from v1 to v2")
		return r.save()
	}

	if *probe.Version != jsonCloudSaveVersion {
		return fmt.Errorf("unknown schema version %d", *probe.Version)
	}

	var f jsonCloudSaveFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("parse v2: %w", err)
	}
	for uid, rec := range f.Users {
		r.data[uid] = userRecordToState(rec)
	}
	return nil
}

func (r *JSONRepository) migrateV1(raw []byte) error {
	var v1 map[string]struct {
		CreditAll     int64     `json:"creditAll"`
		LastAttemptAt time.Time `json:"lastAttemptAt"`
	}
	if err := json.Unmarshal(raw, &v1); err != nil {
		return err
	}
	for uid, rec := range v1 {
		r.data[uid] = autosave.State{
			CreditAll:     rec.CreditAll,
			LastAttemptAt: rec.LastAttemptAt,
			DataHash:      "",
		}
	}
	return nil
}

func (r *JSONRepository) save() error {
	users := make(map[string]jsonUserRecord, len(r.data))
	for uid, s := range r.data {
		users[uid] = jsonUserRecord{
			CreditAll:     s.CreditAll,
			LastAttemptAt: s.LastAttemptAt.UTC(),
			DataHash:      s.DataHash,
		}
	}

	b, err := json.MarshalIndent(jsonCloudSaveFile{Version: jsonCloudSaveVersion, Users: users}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, r.path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func userRecordToState(rec jsonUserRecord) autosave.State {
	return autosave.State{
		CreditAll:     rec.CreditAll,
		LastAttemptAt: rec.LastAttemptAt,
		DataHash:      rec.DataHash,
	}
}
