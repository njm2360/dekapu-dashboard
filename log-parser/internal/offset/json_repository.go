package offset

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"log-parser/internal/watcher"
)

var _ watcher.OffsetRepository = (*JSONRepository)(nil)

type JSONRepository struct {
	path string
}

func NewJSONRepository(path string) *JSONRepository {
	if path == "" {
		exe, _ := os.Executable()
		path = filepath.Join(filepath.Dir(exe), "state.json")
	}
	return &JSONRepository{path: path}
}

func (r *JSONRepository) Load() (map[string]int64, error) {
	f, err := os.Open(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]int64), nil
		}
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	var m map[string]int64
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	log.Printf("[OffsetRepo] Loaded %s (%d entries)", r.path, len(m))
	return m, nil
}

func (r *JSONRepository) Save(offsets map[string]int64) error {
	tmp := r.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(offsets); err != nil {
		f.Close()
		return fmt.Errorf("encode: %w", err)
	}
	f.Close()
	if err := os.Rename(tmp, r.path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
