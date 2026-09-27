// Package config loads the calendar-sync configuration.
//
// Calendar IDs are email addresses, so they are not kept in the config file.
// The hub ID comes from the HUB_CALENDAR_ID environment variable and each
// source's ID from CALENDAR_ID_<NAME>, with the name upper-cased.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// EnvHub names the environment variable holding the hub calendar ID.
const EnvHub = "HUB_CALENDAR_ID"

// Source is one calendar mirrored onto the hub.
type Source struct {
	Name    string `yaml:"name"`
	Prefix  string `yaml:"prefix"`
	ColorID string `yaml:"color_id"`
	ID      string `yaml:"-"`
}

// EnvVar names the environment variable holding this source's calendar ID.
func (s Source) EnvVar() string {
	return "CALENDAR_ID_" + strings.ToUpper(s.Name)
}

// Config is the whole configuration.
type Config struct {
	SyncDays       int      `yaml:"sync_days"`
	FullResyncDays int      `yaml:"full_resync_days"`
	Sources        []Source `yaml:"sources"`
	HubCalendarID  string   `yaml:"-"`
}

// Load reads the configuration at path, fills in calendar IDs from getenv,
// and validates the result.
func Load(path string, getenv func(string) string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.HubCalendarID = getenv(EnvHub)
	for i := range cfg.Sources {
		cfg.Sources[i].ID = getenv(cfg.Sources[i].EnvVar())
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return &cfg, nil
}

// Validate checks required fields and that the hub is never a source.
func (c *Config) Validate() error {
	if c.HubCalendarID == "" {
		return fmt.Errorf("%s is required", EnvHub)
	}
	if c.SyncDays <= 0 {
		return errors.New("sync_days must be positive")
	}
	if c.FullResyncDays <= 0 {
		return errors.New("full_resync_days must be positive")
	}
	if len(c.Sources) == 0 {
		return errors.New("at least one source is required")
	}
	names := make(map[string]bool, len(c.Sources))
	ids := make(map[string]bool, len(c.Sources))
	for i, s := range c.Sources {
		if s.Name == "" {
			return fmt.Errorf("source %d: name is required", i)
		}
		if names[s.Name] {
			return fmt.Errorf("source %q: duplicate name", s.Name)
		}
		names[s.Name] = true
		if s.ID == "" {
			return fmt.Errorf("source %q: %s is required", s.Name, s.EnvVar())
		}
		if s.ID == c.HubCalendarID {
			return fmt.Errorf("source %q: the hub calendar cannot be a source", s.Name)
		}
		if ids[s.ID] {
			return fmt.Errorf("source %q: duplicate calendar id", s.Name)
		}
		ids[s.ID] = true
	}
	return nil
}
