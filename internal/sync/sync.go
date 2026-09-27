// Package sync mirrors source calendars onto the hub calendar.
package sync

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/api/calendar/v3"

	"github.com/codeallthethingz/calendar-sync/internal/config"
	"github.com/codeallthethingz/calendar-sync/internal/gcal"
	"github.com/codeallthethingz/calendar-sync/internal/state"
)

// Syncer runs sync passes. All fields are required.
type Syncer struct {
	API    gcal.EventsAPI
	Config *config.Config
	Now    func() time.Time
	Log    *log.Logger
}

// Run syncs every source, updating st in place for each source that
// succeeds. It attempts all sources and returns an error if any failed.
func (s *Syncer) Run(ctx context.Context, st state.State) error {
	var failed []string
	for _, src := range s.Config.Sources {
		next, err := s.syncSource(ctx, src, st[src.Name])
		if err != nil {
			s.Log.Printf("source=%s error: %v", src.Name, err)
			failed = append(failed, src.Name)
			continue
		}
		st[src.Name] = next
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d of %d sources failed: %s", len(failed), len(s.Config.Sources), strings.Join(failed, ", "))
	}
	return nil
}

func (s *Syncer) syncSource(ctx context.Context, src config.Source, prev state.Source) (state.Source, error) {
	now := s.Now()
	if s.fullSyncDue(prev, now) {
		return s.fullSync(ctx, src, now)
	}
	next, err := s.incrementalSync(ctx, src, prev)
	if errors.Is(err, gcal.ErrSyncTokenGone) {
		s.Log.Printf("source=%s sync token expired, running full sync", src.Name)
		return s.fullSync(ctx, src, now)
	}
	return next, err
}

func (s *Syncer) fullSyncDue(prev state.Source, now time.Time) bool {
	if prev.SyncToken == "" {
		return true
	}
	interval := time.Duration(s.Config.FullResyncDays) * 24 * time.Hour
	return now.Sub(prev.LastFullSync) >= interval
}

func (s *Syncer) fullSync(ctx context.Context, src config.Source, now time.Time) (state.Source, error) {
	s.Log.Printf("source=%s full sync", src.Name)
	q := gcal.ListQuery{TimeMin: now, TimeMax: now.AddDate(0, 0, s.Config.SyncDays)}
	events, token, err := s.API.ListEvents(ctx, src.ID, q)
	if err != nil {
		return state.Source{}, fmt.Errorf("full sync: %w", err)
	}
	seen := make(map[string]bool, len(events))
	for _, ev := range events {
		if reason := skipReason(ev); reason != "" {
			s.logAction(src, "skip", ev.Id, reason)
			continue
		}
		if err := s.upsert(ctx, src, ev); err != nil {
			return state.Source{}, fmt.Errorf("full sync: %w", err)
		}
		seen[ev.Id] = true
	}
	if err := s.deleteOrphans(ctx, src, seen, now); err != nil {
		return state.Source{}, fmt.Errorf("full sync: %w", err)
	}
	return state.Source{SyncToken: token, LastFullSync: now}, nil
}

func (s *Syncer) incrementalSync(ctx context.Context, src config.Source, prev state.Source) (state.Source, error) {
	events, token, err := s.API.ListEvents(ctx, src.ID, gcal.ListQuery{SyncToken: prev.SyncToken})
	if err != nil {
		return state.Source{}, fmt.Errorf("incremental sync: %w", err)
	}
	if len(events) == 0 {
		// Nothing changed. Keep the old token so the state file stays
		// byte-identical and the workflow has nothing to commit.
		return prev, nil
	}
	now := s.Now()
	windowEnd := now.AddDate(0, 0, s.Config.SyncDays)
	for _, ev := range events {
		if err := s.applyChange(ctx, src, ev, now, windowEnd); err != nil {
			return state.Source{}, fmt.Errorf("incremental sync: %w", err)
		}
	}
	return state.Source{SyncToken: token, LastFullSync: prev.LastFullSync}, nil
}

// applyChange applies one incremental change. The sync token carries the
// window of the initial full sync, but an edit to a recurring event returns
// every instance regardless, so the window is enforced here too.
func (s *Syncer) applyChange(ctx context.Context, src config.Source, ev *calendar.Event, windowStart, windowEnd time.Time) error {
	if reason := skipReason(ev); reason != "" {
		return s.deleteCopies(ctx, src, ev.Id, reason)
	}
	if endedBefore(ev, windowStart) {
		// Already over. Leave any existing copy alone so the hub keeps history.
		s.logAction(src, "skip", ev.Id, "ended before window")
		return nil
	}
	if startsAfter(ev, windowEnd) {
		return s.deleteCopies(ctx, src, ev.Id, "starts after window")
	}
	return s.upsert(ctx, src, ev)
}

func (s *Syncer) upsert(ctx context.Context, src config.Source, ev *calendar.Event) error {
	hubID := s.Config.HubCalendarID
	existing, err := s.API.FindTagged(ctx, hubID, tags(src.ID, ev.Id))
	if err != nil {
		return fmt.Errorf("look up hub copy of %s: %w", ev.Id, err)
	}
	want := hubEvent(src, ev)
	if len(existing) > 0 {
		if err := s.API.PatchEvent(ctx, hubID, existing[0].Id, want); err != nil {
			return fmt.Errorf("patch hub copy of %s: %w", ev.Id, err)
		}
		s.logAction(src, "patch", ev.Id, "")
		return nil
	}
	if err := s.API.InsertEvent(ctx, hubID, want); err != nil {
		return fmt.Errorf("insert hub copy of %s: %w", ev.Id, err)
	}
	s.logAction(src, "insert", ev.Id, "")
	return nil
}

func (s *Syncer) deleteCopies(ctx context.Context, src config.Source, sourceEventID, reason string) error {
	hubID := s.Config.HubCalendarID
	existing, err := s.API.FindTagged(ctx, hubID, tags(src.ID, sourceEventID))
	if err != nil {
		return fmt.Errorf("look up hub copy of %s: %w", sourceEventID, err)
	}
	if len(existing) == 0 {
		s.logAction(src, "skip", sourceEventID, reason+", no hub copy")
		return nil
	}
	for _, hub := range existing {
		if err := s.API.DeleteEvent(ctx, hubID, hub.Id); err != nil {
			return fmt.Errorf("delete hub copy of %s: %w", sourceEventID, err)
		}
		s.logAction(src, "delete", sourceEventID, reason)
	}
	return nil
}

// deleteOrphans removes hub copies for this source whose source event was
// not in the full listing. Copies that ended before the window started are
// kept: the listing starts at now, so they are absent from it without having
// been deleted at source.
func (s *Syncer) deleteOrphans(ctx context.Context, src config.Source, seen map[string]bool, windowStart time.Time) error {
	hubID := s.Config.HubCalendarID
	tagged, err := s.API.FindTagged(ctx, hubID, map[string]string{propSourceCalendar: src.ID})
	if err != nil {
		return fmt.Errorf("list hub events for source: %w", err)
	}
	for _, hub := range tagged {
		sourceEventID := sourceEventOf(hub)
		if seen[sourceEventID] || endedBefore(hub, windowStart) {
			continue
		}
		if err := s.API.DeleteEvent(ctx, hubID, hub.Id); err != nil {
			return fmt.Errorf("delete orphan %s: %w", hub.Id, err)
		}
		s.logAction(src, "delete", sourceEventID, "not in source")
	}
	return nil
}

// endedBefore reports whether ev's end is known and falls before t.
func endedBefore(ev *calendar.Event, t time.Time) bool {
	if ev.End == nil {
		return false
	}
	if ev.End.DateTime != "" {
		end, err := time.Parse(time.RFC3339, ev.End.DateTime)
		return err == nil && end.Before(t)
	}
	if ev.End.Date != "" {
		// All-day end dates are exclusive: the event ended at 00:00 on this date.
		end, err := time.Parse("2006-01-02", ev.End.Date)
		return err == nil && !end.After(t)
	}
	return false
}

// startsAfter reports whether ev's start is known and falls after t.
func startsAfter(ev *calendar.Event, t time.Time) bool {
	if ev.Start == nil {
		return false
	}
	if ev.Start.DateTime != "" {
		start, err := time.Parse(time.RFC3339, ev.Start.DateTime)
		return err == nil && start.After(t)
	}
	if ev.Start.Date != "" {
		start, err := time.Parse("2006-01-02", ev.Start.Date)
		return err == nil && start.After(t)
	}
	return false
}

func sourceEventOf(ev *calendar.Event) string {
	if ev.ExtendedProperties == nil {
		return ""
	}
	return ev.ExtendedProperties.Private[propSourceEvent]
}

func (s *Syncer) logAction(src config.Source, action, sourceEventID, reason string) {
	if reason == "" {
		s.Log.Printf("source=%s action=%s event=%s", src.Name, action, sourceEventID)
		return
	}
	s.Log.Printf("source=%s action=%s event=%s reason=%q", src.Name, action, sourceEventID, reason)
}
