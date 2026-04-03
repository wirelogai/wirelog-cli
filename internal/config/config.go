// Package config handles CLI configuration loading, saving, and precedence resolution.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DefaultHost is the default WireLog API host.
	DefaultHost = "https://api.wirelog.ai"

	envAPIKey = "WIRELOG_API_KEY"
	envHost   = "WIRELOG_HOST"

	globalDir      = "wirelog"
	globalFile     = "config.json"
	projectFile    = ".wirelog.json"
	xdgConfigHome  = "XDG_CONFIG_HOME"
	defaultXDGBase = ".config"
)

// Config holds CLI configuration persisted to disk.
type Config struct {
	APIKey string `json:"api_key,omitempty"`
	Host   string `json:"host,omitempty"`
}

// Resolved holds the final resolved configuration after applying
// the full precedence chain: flags > env > project-local > global > defaults.
type Resolved struct {
	APIKey string
	Host   string
	Source string // description of where the api key came from
}

// Resolve builds the final configuration by applying the precedence chain.
// flagKey and flagHost are values from CLI flags (empty string means not set).
func Resolve(flagKey, flagHost string) *Resolved {
	r := &Resolved{Host: DefaultHost}

	// Layer 1: global config file (~/.config/wirelog/config.json)
	if gc, err := loadFile(GlobalPath()); err == nil {
		if gc.APIKey != "" {
			r.APIKey = gc.APIKey
			r.Source = "global config"
		}
		if gc.Host != "" {
			r.Host = gc.Host
		}
	}

	// Layer 2: project-local config (.wirelog.json in cwd)
	if pc, err := loadFile(ProjectPath()); err == nil {
		if pc.APIKey != "" {
			r.APIKey = pc.APIKey
			r.Source = "project config"
		}
		if pc.Host != "" {
			r.Host = pc.Host
		}
	}

	// Layer 3: environment variables
	if v := os.Getenv(envAPIKey); v != "" {
		r.APIKey = v
		r.Source = "WIRELOG_API_KEY"
	}
	if v := os.Getenv(envHost); v != "" {
		r.Host = v
	}

	// Layer 4: CLI flags (highest priority)
	if flagKey != "" {
		r.APIKey = flagKey
		r.Source = "--api-key flag"
	}
	if flagHost != "" {
		r.Host = flagHost
	}

	// Normalize host: strip trailing slash
	r.Host = strings.TrimRight(r.Host, "/")

	return r
}

// Save writes the config to the global config file.
func Save(cfg *Config) error {
	p := GlobalPath()
	dir := filepath.Dir(p)

	err := os.MkdirAll(dir, 0o700)
	if err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	data = append(data, '\n')

	err = os.WriteFile(p, data, 0o600)
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

// Load reads the global config file. Returns an empty config if the file does not exist.
func Load() (*Config, error) {
	cfg, err := loadFile(GlobalPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, err
	}
	return cfg, nil
}

// GlobalPath returns the path to the global config file.
func GlobalPath() string {
	base := os.Getenv(xdgConfigHome)
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".", globalDir, globalFile)
		}
		base = filepath.Join(home, defaultXDGBase)
	}
	return filepath.Join(base, globalDir, globalFile)
}

// ProjectPath returns the path to the project-local config file.
func ProjectPath() string {
	cwd, err := os.Getwd()
	if err != nil {
		return projectFile
	}
	return filepath.Join(cwd, projectFile)
}

// MaskKey returns a masked version of an API key for display.
// Shows the prefix and last 4 characters.
func MaskKey(key string) string {
	if key == "" {
		return "(not set)"
	}
	if len(key) <= 8 {
		return key[:2] + "****"
	}
	// Show prefix (e.g., "sk_") and last 4 chars
	prefixEnd := 3
	if idx := strings.Index(key, "_"); idx >= 0 && idx < 5 {
		prefixEnd = idx + 1
	}
	return key[:prefixEnd] + "****" + key[len(key)-4:]
}

func loadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	err = json.Unmarshal(data, &cfg)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &cfg, nil
}
