package config

import (
	"os"
	"path/filepath"
)

// SavedConfigPath returns the path where proxy config is persisted.
// Also used to derive the gateway.db path (same directory).
func SavedConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".recall-mcp-gateway", "config.yaml")
}

// LoadSavedConfig loads the previously saved proxy config.
func LoadSavedConfig() (*Config, error) {
	return Load(SavedConfigPath())
}

// HasSavedConfig returns true if a saved config exists.
func HasSavedConfig() bool {
	_, err := os.Stat(SavedConfigPath())
	return err == nil
}
