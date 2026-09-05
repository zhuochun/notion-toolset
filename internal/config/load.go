package config

import (
	"fmt"
	"os"

	"github.com/go-yaml/yaml"
)

// Load preserves runtime YAML permissiveness and the config-free single-ID path.
func Load(path string, one string) (Config, error) {
	var cfg Config
	if path == "" && one != "" {
		return cfg, nil
	}
	err := decodeFile(path, &cfg)
	return cfg, err
}

func LoadMulti(path string) ([]Config, error) {
	configs := []Config{}
	err := decodeFile(path, &configs)
	return configs, err
}

func decodeFile(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Error in Config File (%v): %v", path, err)
	}
	if err := yaml.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("Error in unmarshal Config: %v", err)
	}
	return nil
}
