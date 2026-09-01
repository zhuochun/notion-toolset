package retry

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestDoIfContextStopsDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sentinel := errors.New("temporary failure")
	attempts := 0

	err := doIfContext(
		ctx,
		func() error {
			attempts++
			return sentinel
		},
		func(error) bool { return true },
		func(context.Context, time.Duration) error {
			cancel()
			return ctx.Err()
		},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected one attempt, got %d", attempts)
	}
}

func TestDoIfRetriesWithExponentialBackoff(t *testing.T) {
	sentinel := errors.New("temporary failure")
	attempts := 0
	delays := []time.Duration{}

	err := doIf(
		func() error {
			attempts++
			return sentinel
		},
		func(error) bool { return true },
		func(delay time.Duration) { delays = append(delays, delay) },
	)

	if !errors.Is(err, sentinel) {
		t.Fatalf("expected final error %v, got %v", sentinel, err)
	}
	if attempts != Count {
		t.Fatalf("expected %d attempts, got %d", Count, attempts)
	}

	wantDelays := []time.Duration{Delay, 2 * Delay, 4 * Delay, 8 * Delay}
	if !reflect.DeepEqual(delays, wantDelays) {
		t.Fatalf("expected delays %v, got %v", wantDelays, delays)
	}
}

func TestDoIfStopsAfterSuccess(t *testing.T) {
	attempts := 0
	delays := []time.Duration{}

	err := doIf(
		func() error {
			attempts++
			if attempts < 3 {
				return errors.New("temporary failure")
			}
			return nil
		},
		func(error) bool { return true },
		func(delay time.Duration) { delays = append(delays, delay) },
	)

	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}

	wantDelays := []time.Duration{Delay, 2 * Delay}
	if !reflect.DeepEqual(delays, wantDelays) {
		t.Fatalf("expected delays %v, got %v", wantDelays, delays)
	}
}

func TestDoIfDoesNotRetryPermanentError(t *testing.T) {
	sentinel := errors.New("permanent failure")
	attempts := 0
	delays := 0

	err := doIf(
		func() error {
			attempts++
			return sentinel
		},
		func(error) bool { return false },
		func(time.Duration) { delays++ },
	)

	if !errors.Is(err, sentinel) {
		t.Fatalf("expected final error %v, got %v", sentinel, err)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
	if delays != 0 {
		t.Fatalf("expected no delays, got %d", delays)
	}
}
