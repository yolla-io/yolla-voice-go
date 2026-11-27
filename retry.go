package yollavoice

import (
	"context"
	"time"
)

const (
	baseBackoff   = 500 * time.Millisecond
	maxBackoff    = 8 * time.Second
	maxRetryAfter = 60 * time.Second
)

// backoff returns the delay before the retry that follows attempt (1-based). A positive retryAfter
// (capped at 60 s) replaces the full-jitter exponential backoff from 0.5 s capped at 8 s.
func backoff(attempt int, retryAfter time.Duration, rand func() float64) time.Duration {
	if retryAfter > 0 {
		return min(retryAfter, maxRetryAfter)
	}
	ceiling := maxBackoff
	if attempt = max(attempt, 1); attempt < 6 {
		ceiling = min(baseBackoff<<(attempt-1), maxBackoff)
	}
	return time.Duration(rand() * float64(ceiling))
}

// sleepContext waits for d or until ctx is done, returning ctx.Err() in that case.
func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
