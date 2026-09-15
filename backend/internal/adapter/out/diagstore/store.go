package diagstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/bytecode/modbus-mapping-gateway/internal/domain"
)

// Store persists diagnostics state (sample ring + sampler states) as
// one JSON document, written atomically via tmp+rename.
type Store struct {
	path string
	mu   sync.Mutex
}

func New(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Path() string { return s.path }

func (s *Store) Load() (domain.DiagState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path)
	if err != nil {
		return domain.DiagState{}, err
	}
	var st domain.DiagState
	if err := json.Unmarshal(b, &st); err != nil {
		return domain.DiagState{}, fmt.Errorf("diag state parse: %w", err)
	}
	return st, nil
}

func (s *Store) Save(state domain.DiagState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
