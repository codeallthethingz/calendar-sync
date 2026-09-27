package sync

import (
	"context"
	"fmt"
	"sort"

	"google.golang.org/api/calendar/v3"

	"github.com/codeallthethingz/calendar-sync/internal/gcal"
)

// incremental is the scripted response to a sync-token query.
type incremental struct {
	events []*calendar.Event
	next   string
	gone   bool
}

// fakeAPI is an in-memory Calendar API. Source calendars are scripted; the
// hub is a map of stored events.
type fakeAPI struct {
	full      map[string][]*calendar.Event // calendar ID -> events for a full list
	fullToken map[string]string            // calendar ID -> nextSyncToken for a full list
	incr      map[string]incremental       // sync token -> response
	listErr   map[string]error             // calendar ID -> error from ListEvents

	hub    map[string]*calendar.Event
	nextID int

	queries []gcal.ListQuery
	inserts int
	patches int
}

func newFake() *fakeAPI {
	return &fakeAPI{
		full:      map[string][]*calendar.Event{},
		fullToken: map[string]string{},
		incr:      map[string]incremental{},
		listErr:   map[string]error{},
		hub:       map[string]*calendar.Event{},
	}
}

func (f *fakeAPI) ListEvents(_ context.Context, calendarID string, q gcal.ListQuery) ([]*calendar.Event, string, error) {
	f.queries = append(f.queries, q)
	if err := f.listErr[calendarID]; err != nil {
		return nil, "", err
	}
	if q.SyncToken == "" {
		return f.full[calendarID], f.fullToken[calendarID], nil
	}
	r, ok := f.incr[q.SyncToken]
	if !ok {
		return nil, "", fmt.Errorf("unexpected sync token %q", q.SyncToken)
	}
	if r.gone {
		return nil, "", fmt.Errorf("list: %w", gcal.ErrSyncTokenGone)
	}
	return r.events, r.next, nil
}

func (f *fakeAPI) FindTagged(_ context.Context, _ string, props map[string]string) ([]*calendar.Event, error) {
	var out []*calendar.Event
	for _, ev := range f.hub {
		if matches(ev, props) {
			out = append(out, ev)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Id < out[j].Id })
	return out, nil
}

func matches(ev *calendar.Event, props map[string]string) bool {
	if ev.ExtendedProperties == nil {
		return false
	}
	for k, v := range props {
		if ev.ExtendedProperties.Private[k] != v {
			return false
		}
	}
	return true
}

func (f *fakeAPI) InsertEvent(_ context.Context, _ string, ev *calendar.Event) error {
	f.nextID++
	cp := *ev
	cp.Id = fmt.Sprintf("hub%d", f.nextID)
	f.hub[cp.Id] = &cp
	f.inserts++
	return nil
}

func (f *fakeAPI) PatchEvent(_ context.Context, _ string, eventID string, ev *calendar.Event) error {
	if _, ok := f.hub[eventID]; !ok {
		return fmt.Errorf("patch: no hub event %s", eventID)
	}
	cp := *ev
	cp.Id = eventID
	f.hub[eventID] = &cp
	f.patches++
	return nil
}

func (f *fakeAPI) DeleteEvent(_ context.Context, _ string, eventID string) error {
	delete(f.hub, eventID)
	return nil
}

// seedHub stores a hub event tagged with the given source.
func (f *fakeAPI) seedHub(id, sourceCalendarID, sourceEventID string) {
	f.hub[id] = &calendar.Event{
		Id:                 id,
		Summary:            "old",
		ExtendedProperties: &calendar.EventExtendedProperties{Private: tags(sourceCalendarID, sourceEventID)},
	}
}

// hubCopies returns the hub events copied from one source event.
func (f *fakeAPI) hubCopies(sourceCalendarID, sourceEventID string) []*calendar.Event {
	out, _ := f.FindTagged(context.Background(), "", tags(sourceCalendarID, sourceEventID))
	return out
}
