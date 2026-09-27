package sync

import "google.golang.org/api/calendar/v3"

// Reasons an event is kept off the hub.
const (
	reasonCancelled = "cancelled"
	reasonDeclined  = "declined"
	reasonFree      = "marked free"
)

// skipReason returns why ev must not block time on the hub, or "" if it does.
// All-day events are exempt from the Free rule: they are copied and marked
// Free on the hub instead.
func skipReason(ev *calendar.Event) string {
	switch {
	case ev.Status == "cancelled":
		return reasonCancelled
	case declinedBySelf(ev):
		return reasonDeclined
	case !isAllDay(ev) && ev.Transparency == "transparent":
		return reasonFree
	}
	return ""
}

func declinedBySelf(ev *calendar.Event) bool {
	for _, a := range ev.Attendees {
		if a.Self && a.ResponseStatus == "declined" {
			return true
		}
	}
	return false
}

func isAllDay(ev *calendar.Event) bool {
	return ev.Start != nil && ev.Start.Date != ""
}
