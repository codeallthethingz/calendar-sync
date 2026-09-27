package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLoadMissingIsFresh(t *testing.T) {
	st, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(st) != 0 {
		t.Fatalf("got %v, want empty", st)
	}
}

func TestLoadEmptyIsFresh(t *testing.T) {
	for _, body := range []string{"", "  \n", "{}\n"} {
		path := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		st, err := Load(path)
		if err != nil {
			t.Fatalf("Load(%q): %v", body, err)
		}
		if st == nil || len(st) != 0 {
			t.Fatalf("Load(%q) = %v, want empty non-nil", body, st)
		}
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := State{
		"a@example.com": {SyncToken: "tok-a", LastFullSync: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)},
		"b@example.com": {SyncToken: "tok-b", LastFullSync: time.Date(2026, 9, 20, 8, 30, 0, 0, time.UTC)},
	}
	if err := want.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip: got %v, want %v", got, want)
	}
}

func TestLoadRejectsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected parse error")
	}
}
