package gcal

import (
	"context"
	"errors"
	"net/http"
	"time"

	"google.golang.org/api/googleapi"
)

const (
	maxAttempts  = 5
	initialDelay = time.Second
)

type sleepFunc func(ctx context.Context, d time.Duration) error

// retry calls fn up to maxAttempts times, backing off exponentially while
// the error is a rate-limit response.
func retry(ctx context.Context, sleep sleepFunc, fn func() error) error {
	delay := initialDelay
	for attempt := 1; ; attempt++ {
		err := fn()
		if err == nil || !isRateLimited(err) || attempt == maxAttempts {
			return err
		}
		if serr := sleep(ctx, delay); serr != nil {
			return serr
		}
		delay *= 2
	}
}

func isRateLimited(err error) bool {
	var gerr *googleapi.Error
	if !errors.As(err, &gerr) {
		return false
	}
	if gerr.Code == http.StatusTooManyRequests {
		return true
	}
	if gerr.Code != http.StatusForbidden {
		return false
	}
	for _, item := range gerr.Errors {
		if item.Reason == "rateLimitExceeded" || item.Reason == "userRateLimitExceeded" {
			return true
		}
	}
	return false
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
