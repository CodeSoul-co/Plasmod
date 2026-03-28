// Package config provides YAML-driven configuration loading for all algorithm
// packages. It follows a priority order: YAML file → environment variable → default.
//
// All algorithm packages should use LoadYAML from this package to load their
// configuration. This ensures a consistent config loading pattern across the codebase.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

// LoadYAML reads a YAML config file and unmarshals it into dest.
// If the file does not exist, no error is returned and dest is unchanged
// (caller should initialise dest with code defaults before calling).
// If the file exists but unmarshalling fails, an error is returned.
func LoadYAML(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // file absent → use caller's code defaults
		}
		return fmt.Errorf("read config file %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("unmarshal config file %q: %w", path, err)
	}
	return nil
}
