package retry

import (
	"context"
	"time"
)

const Count = 5
const Delay = time.Second

func Do(fn func() error) error {
	return DoIf(fn, func(error) bool { return true })
}

func DoIf(fn func() error, shouldRetry func(error) bool) error {
	return doIf(fn, shouldRetry, time.Sleep)
}

// DoIfContext retries fn while allowing the caller to cancel backoff waits.
func DoIfContext(ctx context.Context, fn func() error, shouldRetry func(error) bool) error {
	return doIfContext(ctx, fn, shouldRetry, waitContext)
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func doIfContext(ctx context.Context, fn func() error, shouldRetry func(error) bool, wait func(context.Context, time.Duration) error) error {
	var err error
	delay := Delay

	for attempt := 0; attempt < Count; attempt++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = fn(); err == nil {
			return nil
		}
		if !shouldRetry(err) || attempt == Count-1 {
			return err
		}
		if err = wait(ctx, delay); err != nil {
			return err
		}
		delay *= 2
	}

	return err
}

func doIf(fn func() error, shouldRetry func(error) bool, wait func(time.Duration)) error {
	return doIfContext(context.Background(), fn, shouldRetry, func(_ context.Context, delay time.Duration) error {
		wait(delay)
		return nil
	})
}
