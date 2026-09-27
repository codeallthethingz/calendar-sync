package sync

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/calendar/v3"

	"github.com/codeallthethingz/calendar-sync/internal/config"
	"github.com/codeallthethingz/calendar-sync/internal/state"
)

const (
	hubID = "hub@example.com"
	srcID = "massage@example.com"
)

var now = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

func testConfig(ids ...string) *config.Config {
	cfg := &config.Config{HubCalendarID: hubID, SyncDays: 90, FullResyncDays: 30}
	for _, id := range ids {
		cfg.Sources = append(cfg.Sources, config.Source{Name: id, ID: id, Prefix: "massage: ", ColorID: "6"})
	}
	return cfg
}

func newSyncer(api *fakeAPI, cfg *config.Config) (*Syncer, *bytes.Buffer) {
	var logs bytes.Buffer
	return &Syncer{API: api, Config: cfg, Now: func() time.Time { return now }, Log: log.New(&logs, "", 0)}, &logs
}

func timed(id string) *calendar.Event {
	return &calendar.Event{
		Id:      id,
		Status:  "confirmed",
		Summary: "event " + id,
		Start:   &calendar.EventDateTime{DateTime: "2026-09-26T10:00:00-04:00", TimeZone: "America/New_York"},
		End:     &calendar.EventDateTime{DateTime: "2026-09-26T11:00:00-04:00", TimeZone: "America/New_York"},
	}
}

func declined(id string) *calendar.Event {
	ev := timed(id)
	ev.Attendees = []*calendar.EventAttendee{{Email: "me@example.com", Self: true, ResponseStatus: "declined"}}
	return ev
}

func TestFullSyncInsertsPatchesAndDeletesOrphans(t *testing.T) {
	api := newFake()
	free := timed("free")
	free.Transparency = "transparent"
	allDay := &calendar.Event{
		Id: "allday", Status: "confirmed", Summary: "holiday", Transparency: "transparent",
		Start: &calendar.EventDateTime{Date: "2026-09-27"}, End: &calendar.EventDateTime{Date: "2026-09-28"},
	}
	api.full[srcID] = []*calendar.Event{timed("keep"), timed("new"), declined("no"), free, allDay}
	api.fullToken[srcID] = "tok1"
	api.seedHub("h-keep", srcID, "keep")
	api.seedHub("h-orphan", srcID, "gone")
	api.seedHub("h-other", "other@example.com", "x")
	api.hub["h-manual"] = &calendar.Event{Id: "h-manual", Summary: "made by hand"}

	s, logs := newSyncer(api, testConfig(srcID))
	st := state.State{}
	if err := s.Run(context.Background(), st); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := api.hubCopies(srcID, "keep"); len(got) != 1 || got[0].Id != "h-keep" || got[0].Summary != "massage: event keep" {
		t.Errorf("existing copy not patched in place: %+v", got)
	}
	if got := api.hubCopies(srcID, "new"); len(got) != 1 {
		t.Errorf("new event: %d hub copies, want 1", len(got))
	}
	if got := api.hubCopies(srcID, "allday"); len(got) != 1 || got[0].Transparency != "transparent" {
		t.Errorf("all-day event not copied as free: %+v", got)
	}
	for _, id := range []string{"no", "free", "gone"} {
		if got := api.hubCopies(srcID, id); len(got) != 0 {
			t.Errorf("%s: %d hub copies, want 0", id, len(got))
		}
	}
	for _, id := range []string{"h-other", "h-manual"} {
		if _, ok := api.hub[id]; !ok {
			t.Errorf("%s was deleted but belongs to another source or no source", id)
		}
	}
	if api.inserts != 2 || api.patches != 1 {
		t.Errorf("inserts=%d patches=%d, want 2 and 1", api.inserts, api.patches)
	}
	want := state.Source{SyncToken: "tok1", LastFullSync: now}
	if st[srcID] != want {
		t.Errorf("state = %+v, want %+v", st[srcID], want)
	}
	q := api.queries[0]
	if q.SyncToken != "" || !q.TimeMin.Equal(now) || !q.TimeMax.Equal(now.AddDate(0, 0, 90)) {
		t.Errorf("full query = %+v", q)
	}
	for _, line := range []string{
		`action=insert event=new`,
		`action=patch event=keep`,
		`action=skip event=no reason="declined"`,
		`action=skip event=free reason="marked free"`,
		`action=delete event=gone reason="not in source"`,
	} {
		if !strings.Contains(logs.String(), line) {
			t.Errorf("log missing %q:\n%s", line, logs.String())
		}
	}
}

func incrementalState() state.State {
	return state.State{srcID: {SyncToken: "tok1", LastFullSync: now.Add(-24 * time.Hour)}}
}

func TestIncrementalCancelledDeletesCopy(t *testing.T) {
	api := newFake()
	api.seedHub("h1", srcID, "e1")
	api.incr["tok1"] = incremental{events: []*calendar.Event{{Id: "e1", Status: "cancelled"}}, next: "tok2"}

	s, _ := newSyncer(api, testConfig(srcID))
	st := incrementalState()
	if err := s.Run(context.Background(), st); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := api.hub["h1"]; ok {
		t.Error("cancelled event's hub copy was not deleted")
	}
	want := state.Source{SyncToken: "tok2", LastFullSync: now.Add(-24 * time.Hour)}
	if st[srcID] != want {
		t.Errorf("state = %+v, want %+v", st[srcID], want)
	}
	if len(api.queries) != 1 || api.queries[0].SyncToken != "tok1" {
		t.Errorf("queries = %+v, want one incremental query", api.queries)
	}
}

func TestIncrementalDeclinedDeletesCopy(t *testing.T) {
	api := newFake()
	api.seedHub("h1", srcID, "e1")
	api.incr["tok1"] = incremental{events: []*calendar.Event{declined("e1"), timed("e2")}, next: "tok2"}

	s, _ := newSyncer(api, testConfig(srcID))
	st := incrementalState()
	if err := s.Run(context.Background(), st); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := api.hub["h1"]; ok {
		t.Error("declined event's hub copy was not deleted")
	}
	if got := api.hubCopies(srcID, "e2"); len(got) != 1 {
		t.Errorf("e2: %d hub copies, want 1", len(got))
	}
}

func TestIncrementalGoneFallsBackToFullSync(t *testing.T) {
	api := newFake()
	api.incr["tok1"] = incremental{gone: true}
	api.full[srcID] = []*calendar.Event{timed("e1")}
	api.fullToken[srcID] = "fresh"
	api.seedHub("h-orphan", srcID, "stale")

	s, logs := newSyncer(api, testConfig(srcID))
	st := incrementalState()
	if err := s.Run(context.Background(), st); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(api.queries) != 2 || api.queries[0].SyncToken != "tok1" || api.queries[1].SyncToken != "" {
		t.Fatalf("queries = %+v, want incremental then full", api.queries)
	}
	want := state.Source{SyncToken: "fresh", LastFullSync: now}
	if st[srcID] != want {
		t.Errorf("state = %+v, want %+v", st[srcID], want)
	}
	if got := api.hubCopies(srcID, "e1"); len(got) != 1 {
		t.Errorf("e1: %d hub copies, want 1", len(got))
	}
	if _, ok := api.hub["h-orphan"]; ok {
		t.Error("orphan survived the fallback full sync")
	}
	if !strings.Contains(logs.String(), "sync token expired") {
		t.Errorf("log missing fallback line:\n%s", logs.String())
	}
}

func TestFullSyncDueAfterResyncInterval(t *testing.T) {
	api := newFake()
	api.fullToken[srcID] = "fresh"
	s, _ := newSyncer(api, testConfig(srcID))
	st := state.State{srcID: {SyncToken: "tok1", LastFullSync: now.AddDate(0, 0, -30)}}
	if err := s.Run(context.Background(), st); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(api.queries) != 1 || api.queries[0].SyncToken != "" {
		t.Errorf("queries = %+v, want one full query", api.queries)
	}
}

func TestRunContinuesPastFailingSource(t *testing.T) {
	const bad, good = "bad@example.com", "good@example.com"
	api := newFake()
	api.listErr[bad] = errors.New("boom")
	api.full[good] = []*calendar.Event{timed("e1")}
	api.fullToken[good] = "tok-good"

	s, _ := newSyncer(api, testConfig(bad, good))
	st := state.State{bad: {SyncToken: "keep-me", LastFullSync: now}}
	err := s.Run(context.Background(), st)
	if err == nil || !strings.Contains(err.Error(), bad) {
		t.Fatalf("Run error = %v, want failure naming %s", err, bad)
	}
	if st[bad].SyncToken != "keep-me" {
		t.Errorf("failed source's state changed: %+v", st[bad])
	}
	if st[good].SyncToken != "tok-good" {
		t.Errorf("good source not synced: %+v", st[good])
	}
}

func TestFullSyncKeepsCopiesOfEndedEvents(t *testing.T) {
	api := newFake()
	api.full[srcID] = nil
	api.fullToken[srcID] = "tok1"
	api.seedHub("h-ended", srcID, "ended")
	api.hub["h-ended"].End = &calendar.EventDateTime{DateTime: "2026-09-20T11:00:00Z"}
	api.seedHub("h-ended-allday", srcID, "ended-allday")
	api.hub["h-ended-allday"].End = &calendar.EventDateTime{Date: "2026-09-21"}
	api.seedHub("h-future", srcID, "future")
	api.hub["h-future"].End = &calendar.EventDateTime{DateTime: "2026-10-01T11:00:00Z"}
	api.seedHub("h-unknown", srcID, "unknown")

	s, _ := newSyncer(api, testConfig(srcID))
	if err := s.Run(context.Background(), state.State{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, id := range []string{"h-ended", "h-ended-allday"} {
		if _, ok := api.hub[id]; !ok {
			t.Errorf("%s ended before the window and was deleted, want kept", id)
		}
	}
	for _, id := range []string{"h-future", "h-unknown"} {
		if _, ok := api.hub[id]; ok {
			t.Errorf("%s is not in the source and was kept, want deleted", id)
		}
	}
}

func TestIncrementalNoChangesLeavesStateUnchanged(t *testing.T) {
	api := newFake()
	api.incr["tok1"] = incremental{events: nil, next: "tok2"}
	s, _ := newSyncer(api, testConfig(srcID))
	st := incrementalState()
	before := st[srcID]
	if err := s.Run(context.Background(), st); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if st[srcID] != before {
		t.Errorf("state = %+v, want unchanged %+v", st[srcID], before)
	}
}

func TestIncrementalIgnoresChangesOutsideWindow(t *testing.T) {
	api := newFake()
	past := timed("past")
	past.Start.DateTime = "2026-09-20T10:00:00Z"
	past.End.DateTime = "2026-09-20T11:00:00Z"
	far := timed("far")
	far.Start.DateTime = "2027-01-15T10:00:00Z"
	far.End.DateTime = "2027-01-15T11:00:00Z"
	api.seedHub("h-far", srcID, "far")
	api.incr["tok1"] = incremental{events: []*calendar.Event{past, far, timed("in")}, next: "tok2"}

	s, logs := newSyncer(api, testConfig(srcID))
	st := incrementalState()
	if err := s.Run(context.Background(), st); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := api.hubCopies(srcID, "in"); len(got) != 1 {
		t.Errorf("in-window event: %d hub copies, want 1", len(got))
	}
	if got := api.hubCopies(srcID, "past"); len(got) != 0 {
		t.Errorf("past event was copied: %+v", got)
	}
	if got := api.hubCopies(srcID, "far"); len(got) != 0 {
		t.Errorf("event beyond the window kept a hub copy: %+v", got)
	}
	if api.inserts != 1 {
		t.Errorf("inserts=%d, want 1", api.inserts)
	}
	if st[srcID].SyncToken != "tok2" {
		t.Errorf("sync token = %q, want tok2", st[srcID].SyncToken)
	}
	for _, line := range []string{
		`action=skip event=past reason="ended before window"`,
		`action=delete event=far reason="starts after window"`,
	} {
		if !strings.Contains(logs.String(), line) {
			t.Errorf("log missing %q:\n%s", line, logs.String())
		}
	}
}
