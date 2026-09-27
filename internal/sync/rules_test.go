package sync

import (
	"testing"

	"google.golang.org/api/calendar/v3"
)

func TestSkipReason(t *testing.T) {
	timedStart := &calendar.EventDateTime{DateTime: "2026-09-26T10:00:00Z"}
	allDayStart := &calendar.EventDateTime{Date: "2026-09-26"}
	tests := []struct {
		name string
		ev   *calendar.Event
		want string
	}{
		{"busy timed", &calendar.Event{Start: timedStart}, ""},
		{"opaque timed", &calendar.Event{Start: timedStart, Transparency: "opaque"}, ""},
		{"free timed", &calendar.Event{Start: timedStart, Transparency: "transparent"}, reasonFree},
		{"cancelled", &calendar.Event{Status: "cancelled"}, reasonCancelled},
		{"self declined", &calendar.Event{Start: timedStart, Attendees: []*calendar.EventAttendee{
			{Email: "a@example.com", ResponseStatus: "accepted"},
			{Email: "me@example.com", Self: true, ResponseStatus: "declined"},
		}}, reasonDeclined},
		{"other attendee declined", &calendar.Event{Start: timedStart, Attendees: []*calendar.EventAttendee{
			{Email: "a@example.com", ResponseStatus: "declined"},
			{Email: "me@example.com", Self: true, ResponseStatus: "accepted"},
		}}, ""},
		{"self tentative", &calendar.Event{Start: timedStart, Attendees: []*calendar.EventAttendee{
			{Email: "me@example.com", Self: true, ResponseStatus: "tentative"},
		}}, ""},
		{"all-day marked free is still copied", &calendar.Event{Start: allDayStart, Transparency: "transparent"}, ""},
		{"all-day declined", &calendar.Event{Start: allDayStart, Attendees: []*calendar.EventAttendee{
			{Self: true, ResponseStatus: "declined"},
		}}, reasonDeclined},
		{"free/busy only event", &calendar.Event{Id: "x", Status: "confirmed", Start: timedStart}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := skipReason(tt.ev); got != tt.want {
				t.Errorf("skipReason = %q, want %q", got, tt.want)
			}
		})
	}
}
