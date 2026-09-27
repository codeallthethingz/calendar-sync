package gcal

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
)

func rateLimit403() error {
	return &googleapi.Error{Code: 403, Errors: []googleapi.ErrorItem{{Reason: "rateLimitExceeded"}}}
}

func TestRetryBacksOffThenSucceeds(t *testing.T) {
	var delays []time.Duration
	sleep := func(_ context.Context, d time.Duration) error { delays = append(delays, d); return nil }
	calls := 0
	err := retry(context.Background(), sleep, func() error {
		calls++
		switch calls {
		case 1:
			return rateLimit403()
		case 2:
			return &googleapi.Error{Code: 429}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
	want := []time.Duration{time.Second, 2 * time.Second}
	if len(delays) != 2 || delays[0] != want[0] || delays[1] != want[1] {
		t.Errorf("delays = %v, want %v", delays, want)
	}
}

func TestRetryGivesUpAfterFiveTries(t *testing.T) {
	sleep := func(context.Context, time.Duration) error { return nil }
	calls := 0
	err := retry(context.Background(), sleep, func() error { calls++; return &googleapi.Error{Code: 429} })
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 5 {
		t.Errorf("calls = %d, want 5", calls)
	}
}

func TestRetryDoesNotRetryOtherErrors(t *testing.T) {
	sleep := func(context.Context, time.Duration) error { t.Fatal("unexpected sleep"); return nil }
	for _, e := range []error{
		&googleapi.Error{Code: 403, Errors: []googleapi.ErrorItem{{Reason: "forbidden"}}},
		&googleapi.Error{Code: 500},
		errors.New("network"),
	} {
		calls := 0
		_ = retry(context.Background(), sleep, func() error { calls++; return e })
		if calls != 1 {
			t.Errorf("%v: calls = %d, want 1", e, calls)
		}
	}
}
