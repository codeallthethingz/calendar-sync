package sync

import (
	"strings"

	"google.golang.org/api/calendar/v3"

	"github.com/codeallthethingz/calendar-sync/internal/config"
)

// Private extended property keys that tag hub events with their origin.
const (
	propSourceCalendar = "sourceCalendarId"
	propSourceEvent    = "sourceEventId"
)

// untitled is the title used when the source gives none, as happens for a
// calendar shared at free/busy level only.
const untitled = "busy"

// hubEvent builds the hub copy of a source event.
func hubEvent(src config.Source, ev *calendar.Event) *calendar.Event {
	transparency := "opaque"
	if isAllDay(ev) || ev.Transparency == "transparent" {
		transparency = "transparent"
	}
	title := ev.Summary
	if title == "" {
		title = untitled
	}
	return &calendar.Event{
		Summary:      src.Prefix + title,
		ColorId:      src.ColorID,
		Start:        copyTime(ev.Start),
		End:          copyTime(ev.End),
		Description:  description(ev),
		Location:     ev.Location,
		Transparency: transparency,
		ExtendedProperties: &calendar.EventExtendedProperties{
			Private: tags(src.ID, ev.Id),
		},
		// Sent even when empty so a patch clears a field removed at source.
		ForceSendFields: []string{"Summary", "Description", "Location"},
	}
}

// copyTime copies a start or end time. The unused form is sent as null so a
// patch can switch an event between all-day and timed.
func copyTime(t *calendar.EventDateTime) *calendar.EventDateTime {
	if t == nil {
		return nil
	}
	out := &calendar.EventDateTime{Date: t.Date, DateTime: t.DateTime, TimeZone: t.TimeZone}
	if t.Date != "" {
		out.NullFields = []string{"DateTime"}
	} else {
		out.NullFields = []string{"Date"}
	}
	return out
}

// description is the source description with a final "Attendees: " line.
func description(ev *calendar.Event) string {
	names := attendeeNames(ev.Attendees)
	if len(names) == 0 {
		return ev.Description
	}
	line := "Attendees: " + strings.Join(names, ", ")
	if ev.Description == "" {
		return line
	}
	return ev.Description + "\n\n" + line
}

func attendeeNames(attendees []*calendar.EventAttendee) []string {
	var names []string
	for _, a := range attendees {
		switch {
		case a.DisplayName != "":
			names = append(names, a.DisplayName)
		case a.Email != "":
			names = append(names, a.Email)
		}
	}
	return names
}

func tags(sourceCalendarID, sourceEventID string) map[string]string {
	return map[string]string{
		propSourceCalendar: sourceCalendarID,
		propSourceEvent:    sourceEventID,
	}
}
