// Package gcal wraps the Google Calendar API calls calendar-sync needs.
package gcal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

// ErrSyncTokenGone means the server rejected a sync token with 410 Gone.
var ErrSyncTokenGone = errors.New("sync token expired (410 Gone)")

// ListQuery selects events from a source calendar. With SyncToken set it is
// an incremental query and the time bounds are ignored.
type ListQuery struct {
	SyncToken string
	TimeMin   time.Time
	TimeMax   time.Time
}

// EventsAPI is the subset of the Calendar API the sync algorithm uses.
type EventsAPI interface {
	// ListEvents pages through a source calendar and returns every event
	// plus the nextSyncToken from the final page.
	ListEvents(ctx context.Context, calendarID string, q ListQuery) ([]*calendar.Event, string, error)
	// FindTagged returns events whose private extended properties match all of props.
	FindTagged(ctx context.Context, calendarID string, props map[string]string) ([]*calendar.Event, error)
	InsertEvent(ctx context.Context, calendarID string, ev *calendar.Event) error
	PatchEvent(ctx context.Context, calendarID, eventID string, ev *calendar.Event) error
	DeleteEvent(ctx context.Context, calendarID, eventID string) error
}

// Service implements EventsAPI against the real Calendar API.
type Service struct {
	svc   *calendar.Service
	sleep sleepFunc
}

var _ EventsAPI = (*Service)(nil)

// New builds a Service using an authenticated HTTP client.
func New(ctx context.Context, httpClient *http.Client) (*Service, error) {
	svc, err := calendar.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("create calendar service: %w", err)
	}
	return &Service{svc: svc, sleep: sleepCtx}, nil
}

// ListEvents implements EventsAPI.
func (s *Service) ListEvents(ctx context.Context, calendarID string, q ListQuery) ([]*calendar.Event, string, error) {
	var all []*calendar.Event
	pageToken := ""
	for {
		call := s.svc.Events.List(calendarID).Context(ctx).SingleEvents(true).MaxResults(2500)
		if q.SyncToken != "" {
			call = call.SyncToken(q.SyncToken)
		} else {
			call = call.ShowDeleted(false).
				TimeMin(q.TimeMin.Format(time.RFC3339)).
				TimeMax(q.TimeMax.Format(time.RFC3339))
		}
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		var page *calendar.Events
		err := retry(ctx, s.sleep, func() error {
			var err error
			page, err = call.Do()
			return err
		})
		if hasCode(err, http.StatusGone) {
			return nil, "", fmt.Errorf("list events on %s: %w", calendarID, ErrSyncTokenGone)
		}
		if err != nil {
			return nil, "", fmt.Errorf("list events on %s: %w", calendarID, err)
		}
		all = append(all, page.Items...)
		if page.NextPageToken == "" {
			return all, page.NextSyncToken, nil
		}
		pageToken = page.NextPageToken
	}
}

// FindTagged implements EventsAPI.
func (s *Service) FindTagged(ctx context.Context, calendarID string, props map[string]string) ([]*calendar.Event, error) {
	filters := propFilters(props)
	var all []*calendar.Event
	pageToken := ""
	for {
		call := s.svc.Events.List(calendarID).Context(ctx).
			PrivateExtendedProperty(filters...).ShowDeleted(false).MaxResults(2500)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		var page *calendar.Events
		err := retry(ctx, s.sleep, func() error {
			var err error
			page, err = call.Do()
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("find tagged events on %s: %w", calendarID, err)
		}
		all = append(all, page.Items...)
		if page.NextPageToken == "" {
			return all, nil
		}
		pageToken = page.NextPageToken
	}
}

// InsertEvent implements EventsAPI.
func (s *Service) InsertEvent(ctx context.Context, calendarID string, ev *calendar.Event) error {
	call := s.svc.Events.Insert(calendarID, ev).Context(ctx).SendUpdates("none")
	err := retry(ctx, s.sleep, func() error {
		_, err := call.Do()
		return err
	})
	if err != nil {
		return fmt.Errorf("insert event on %s: %w", calendarID, err)
	}
	return nil
}

// PatchEvent implements EventsAPI.
func (s *Service) PatchEvent(ctx context.Context, calendarID, eventID string, ev *calendar.Event) error {
	call := s.svc.Events.Patch(calendarID, eventID, ev).Context(ctx).SendUpdates("none")
	err := retry(ctx, s.sleep, func() error {
		_, err := call.Do()
		return err
	})
	if err != nil {
		return fmt.Errorf("patch event %s on %s: %w", eventID, calendarID, err)
	}
	return nil
}

// DeleteEvent implements EventsAPI. An event that is already gone is not an error.
func (s *Service) DeleteEvent(ctx context.Context, calendarID, eventID string) error {
	call := s.svc.Events.Delete(calendarID, eventID).Context(ctx).SendUpdates("none")
	err := retry(ctx, s.sleep, func() error { return call.Do() })
	if hasCode(err, http.StatusNotFound) || hasCode(err, http.StatusGone) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete event %s on %s: %w", eventID, calendarID, err)
	}
	return nil
}

// CalendarList returns every entry in the account's calendar list.
func (s *Service) CalendarList(ctx context.Context) ([]*calendar.CalendarListEntry, error) {
	var all []*calendar.CalendarListEntry
	pageToken := ""
	for {
		call := s.svc.CalendarList.List().Context(ctx).MaxResults(250)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		var page *calendar.CalendarList
		err := retry(ctx, s.sleep, func() error {
			var err error
			page, err = call.Do()
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("list calendars: %w", err)
		}
		all = append(all, page.Items...)
		if page.NextPageToken == "" {
			return all, nil
		}
		pageToken = page.NextPageToken
	}
}

func propFilters(props map[string]string) []string {
	out := make([]string, 0, len(props))
	for k, v := range props {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

func hasCode(err error, code int) bool {
	var gerr *googleapi.Error
	return errors.As(err, &gerr) && gerr.Code == code
}
