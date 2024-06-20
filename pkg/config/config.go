package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Read reads the file at the given path and returns the values.
func Read(path string) (map[string]any, error) {
	// Read file
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: couldn't read file %s: %w", path, err)
	}

	// Unmarshal bytes
	values := map[string]any{}
	ext := filepath.Ext(path)
	switch {
	case ext == ".json":
		if err := json.Unmarshal(b, &values); err != nil {
			return nil, fmt.Errorf("config: couldn't unmarshal file %s: %w", path, err)
		}
	case ext == ".yaml" || ext == ".yml":
		if err := yaml.Unmarshal(b, &values); err != nil {
			return nil, fmt.Errorf("config: couldn't unmarshal file %s: %w", path, err)
		}
	default:
		return nil, fmt.Errorf("config: unsupported file extension %s", ext)
	}
	return values, nil
}

// Write writes the values to the file at the given path.
func Write(path string, values map[string]any) error {
	// Obtain marshaled bytes
	ext := filepath.Ext(path)
	var b []byte
	switch {
	case ext == ".json":
		b, _ = json.MarshalIndent(values, "", "  ")
	case ext == ".yaml" || ext == ".yml":
		b, _ = yaml.Marshal(values)
	default:
		return fmt.Errorf("config: unsupported file extension %s", ext)
	}

	// Create folder if not exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("config: couldn't create folder %s: %w", filepath.Dir(path), err)
	}

	// Write to file
	if err := os.WriteFile(path, b, 0644); err != nil {
		return fmt.Errorf("config: couldn't write file %s: %w", path, err)
	}
	return nil
}
