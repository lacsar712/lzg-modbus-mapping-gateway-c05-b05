package yamlstore

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/bytecode/modbus-mapping-gateway/internal/domain"
	"gopkg.in/yaml.v3"
)

type Store struct {
	path string
	mu   sync.Mutex
}

func New(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Path() string { return s.path }

func (s *Store) Load() (domain.MappingConfig, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path)
	if err != nil {
		return domain.MappingConfig{}, "", err
	}
	var cfg domain.MappingConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return domain.MappingConfig{}, "", fmt.Errorf("yaml parse: %w", err)
	}
	return cfg, string(b), nil
}

func (s *Store) Save(yamlText string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cfg domain.MappingConfig
	if err := yaml.Unmarshal([]byte(yamlText), &cfg); err != nil {
		return fmt.Errorf("yaml parse: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(yamlText), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// EnsureDefault copies defaultPath to path if missing.
func EnsureDefault(path, defaultPath string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	b, err := os.ReadFile(defaultPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
