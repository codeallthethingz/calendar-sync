package sync

import (
	"reflect"
	"testing"

	"google.golang.org/api/calendar/v3"

	"github.com/codeallthethingz/calendar-sync/internal/config"
)

var massage = config.Source{Name: "massage", ID: "massage@example.com", Prefix: "massage: ", ColorID: "6"}

func TestHubEventTimed(t *testing.T) {
	src := &calendar.Event{
		Id:          "e1",
		Summary:     "Deep tissue",
		Description: "Bring water",
		Location:    "Studio 4",
		Start:       &calendar.EventDateTime{DateTime: "2026-09-26T10:00:00-04:00", TimeZone: "America/New_York"},
		End:         &calendar.EventDateTime{DateTime: "2026-09-26T11:00:00-04:00", TimeZone: "America/New_York"},
		Attendees: []*calendar.EventAttendee{
			{Email: "ann@example.com", DisplayName: "Ann"},
			{Email: "bob@example.com"},
		},
		Organizer: &calendar.EventOrganizer{Email: "ann@example.com"},
	}
	got := hubEvent(massage, src)

	if got.Summary != "massage: Deep tissue" {
		t.Errorf("Summary = %q", got.Summary)
	}
	if got.ColorId != "6" {
		t.Errorf("ColorId = %q", got.ColorId)
	}
	if want := "Bring water\n\nAttendees: Ann, bob@example.com"; got.Description != want {
		t.Errorf("Description = %q, want %q", got.Description, want)
	}
	if got.Location != "Studio 4" {
		t.Errorf("Location = %q", got.Location)
	}
	if got.Transparency != "opaque" {
		t.Errorf("Transparency = %q, want opaque", got.Transparency)
	}
	if got.Start.DateTime != src.Start.DateTime || got.Start.TimeZone != "America/New_York" ||
		got.End.DateTime != src.End.DateTime || got.End.TimeZone != "America/New_York" {
		t.Errorf("times not copied: start %+v end %+v", got.Start, got.End)
	}
	if !reflect.DeepEqual(got.Start.NullFields, []string{"Date"}) {
		t.Errorf("Start.NullFields = %v, want [Date]", got.Start.NullFields)
	}
	if len(got.Attendees) != 0 {
		t.Errorf("Attendees set: %v", got.Attendees)
	}
	if got.Organizer != nil {
		t.Errorf("Organizer set: %v", got.Organizer)
	}
	wantProps := map[string]string{"sourceCalendarId": "massage@example.com", "sourceEventId": "e1"}
	if got.ExtendedProperties == nil || !reflect.DeepEqual(got.ExtendedProperties.Private, wantProps) {
		t.Errorf("ExtendedProperties = %+v, want private %v", got.ExtendedProperties, wantProps)
	}
}

func TestHubEventAllDayIsFree(t *testing.T) {
	src := &calendar.Event{
		Id:           "e2",
		Summary:      "Holiday",
		Transparency: "opaque",
		Start:        &calendar.EventDateTime{Date: "2026-09-27"},
		End:          &calendar.EventDateTime{Date: "2026-09-28"},
	}
	got := hubEvent(massage, src)
	if got.Transparency != "transparent" {
		t.Errorf("Transparency = %q, want transparent", got.Transparency)
	}
	if got.Start.Date != "2026-09-27" || got.End.Date != "2026-09-28" || got.Start.DateTime != "" {
		t.Errorf("dates not copied: start %+v end %+v", got.Start, got.End)
	}
	if !reflect.DeepEqual(got.Start.NullFields, []string{"DateTime"}) {
		t.Errorf("Start.NullFields = %v, want [DateTime]", got.Start.NullFields)
	}
}

func TestHubEventDescriptionOnlyAttendees(t *testing.T) {
	src := &calendar.Event{Id: "e3", Summary: "Call", Attendees: []*calendar.EventAttendee{{DisplayName: "Ann"}}}
	if got := hubEvent(massage, src).Description; got != "Attendees: Ann" {
		t.Errorf("Description = %q", got)
	}
}

func TestHubEventFreeBusyOnlySource(t *testing.T) {
	work := config.Source{Name: "work", ID: "work@example.com", Prefix: "work: ", ColorID: "10"}
	src := &calendar.Event{
		Id:     "fb1",
		Status: "confirmed",
		Start:  &calendar.EventDateTime{DateTime: "2026-09-26T14:00:00Z"},
		End:    &calendar.EventDateTime{DateTime: "2026-09-26T15:00:00Z"},
	}
	if reason := skipReason(src); reason != "" {
		t.Fatalf("bare event skipped: %q", reason)
	}
	got := hubEvent(work, src)
	if got.Summary != "work: busy" {
		t.Errorf("Summary = %q, want %q", got.Summary, "work: busy")
	}
	if got.Description != "" || got.Location != "" {
		t.Errorf("Description %q Location %q, want both empty", got.Description, got.Location)
	}
	if got.Transparency != "opaque" {
		t.Errorf("Transparency = %q, want opaque", got.Transparency)
	}
	if got.ColorId != "10" {
		t.Errorf("ColorId = %q", got.ColorId)
	}
}
