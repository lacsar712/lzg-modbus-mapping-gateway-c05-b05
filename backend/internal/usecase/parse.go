package usecase

import (
	"fmt"

	"github.com/bytecode/modbus-mapping-gateway/internal/domain"
	"gopkg.in/yaml.v3"
)

func parseYAML(text string) (domain.MappingConfig, string, error) {
	var cfg domain.MappingConfig
	if err := yaml.Unmarshal([]byte(text), &cfg); err != nil {
		return domain.MappingConfig{}, "", fmt.Errorf("yaml parse: %w", err)
	}
	return cfg, text, nil
}
