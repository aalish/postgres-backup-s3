package retry

import (
	"context"
	"fmt"
	"time"
)

func Do(ctx context.Context, attempts int, initialDelay, maxDelay time.Duration, fn func(attempt int) error) error {
	return DoWithNotify(ctx, attempts, initialDelay, maxDelay, fn, nil)
}

func DoWithNotify(ctx context.Context, attempts int, initialDelay, maxDelay time.Duration, fn func(attempt int) error, notify func(attempt int, err error, nextDelay time.Duration)) error {
	if attempts < 1 {
		return fmt.Errorf("attempts must be at least 1")
	}

	delay := initialDelay
	if delay <= 0 {
		delay = time.Second
	}
	if maxDelay < delay {
		maxDelay = delay
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := fn(attempt); err == nil {
			return nil
		} else {
			lastErr = err
		}

		if attempt == attempts {
			break
		}

		if notify != nil {
			notify(attempt, lastErr, delay)
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("retry canceled: %w", ctx.Err())
		case <-timer.C:
		}

		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", attempts, lastErr)
}
