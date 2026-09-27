package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

var repoEnv = map[string]string{
	"HUB_CALENDAR_ID":     "hub@example.com",
	"CALENDAR_ID_MASSAGE": "massage@example.com",
	"CALENDAR_ID_WILL":    "will@example.com",
	"CALENDAR_ID_LEGAL":   "legal@example.com",
	"CALENDAR_ID_WORK":    "work@example.com",
}

func TestLoadRepoConfig(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "config.yaml"), fakeEnv(repoEnv))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HubCalendarID != "hub@example.com" {
		t.Errorf("hub = %q", cfg.HubCalendarID)
	}
	if cfg.SyncDays != 90 || cfg.FullResyncDays != 30 {
		t.Errorf("windows = %d/%d, want 90/30", cfg.SyncDays, cfg.FullResyncDays)
	}
	want := map[string][3]string{
		"massage": {"massage: ", "6", "massage@example.com"},
		"will":    {"will: ", "9", "will@example.com"},
		"legal":   {"legal: ", "11", "legal@example.com"},
		"work":    {"work: ", "10", "work@example.com"},
	}
	if len(cfg.Sources) != len(want) {
		t.Fatalf("got %d sources, want %d", len(cfg.Sources), len(want))
	}
	for _, s := range cfg.Sources {
		w, ok := want[s.Name]
		if !ok {
			t.Errorf("unexpected source %q", s.Name)
			continue
		}
		if s.Prefix != w[0] || s.ColorID != w[1] || s.ID != w[2] {
			t.Errorf("source %s: prefix %q color %q id %q, want %q %q %q", s.Name, s.Prefix, s.ColorID, s.ID, w[0], w[1], w[2])
		}
	}
}

func TestLoadRequiresEveryCalendarID(t *testing.T) {
	env := map[string]string{}
	for k, v := range repoEnv {
		env[k] = v
	}
	delete(env, "CALENDAR_ID_LEGAL")
	_, err := Load(filepath.Join("..", "..", "config.yaml"), fakeEnv(env))
	if err == nil || !strings.Contains(err.Error(), "CALENDAR_ID_LEGAL") {
		t.Fatalf("err = %v, want one naming CALENDAR_ID_LEGAL", err)
	}
}

func TestLoadRejectsIDInFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	body := "sync_days: 1\nfull_resync_days: 1\nsources:\n  - name: a\n    id: a@example.com\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, fakeEnv(repoEnv)); err == nil {
		t.Fatal("expected error: calendar ids must not be in the config file")
	}
}

func TestValidateRejectsHubAsSource(t *testing.T) {
	cfg := Config{
		HubCalendarID:  "hub@example.com",
		SyncDays:       90,
		FullResyncDays: 30,
		Sources:        []Source{{Name: "loop", ID: "hub@example.com"}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error when hub is listed as a source")
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	body := "sync_days: 1\nfull_resync_days: 1\nsourcez: []\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, fakeEnv(repoEnv)); err == nil {
		t.Fatal("expected error for unknown field")
	}
}
