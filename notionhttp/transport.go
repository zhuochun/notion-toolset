// Package notionhttp owns Notion request pacing and server-directed cooldowns
// for both reads and writes. Workflow code retains ownership of write effects.
package notionhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/retry"
	"golang.org/x/time/rate"
)

// NewTransport wraps a transport with a shared three-requests-per-second budget
// and Retry-After cooldown. Reuse it for all requests from one Notion client.
// It retries explicit 429/529 rejections, never ambiguous write/network failures.
func NewTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &transport{base: base, limiter: rate.NewLimiter(3, 1), now: time.Now, wait: waitContext}
}

type transport struct {
	base    http.RoundTripper
	limiter *rate.Limiter
	now     func() time.Time
	wait    func(context.Context, time.Duration) error
	mu      sync.Mutex
	resume  time.Time
}

// ExhaustedError preserves the Notion API error while preventing an outer read
// retry loop from starting another batch of rate-limit retries.
type ExhaustedError struct{ Err error }

func (e *ExhaustedError) Error() string { return e.Err.Error() }
func (e *ExhaustedError) Unwrap() error { return e.Err }

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	current := req
	for attempt := 0; attempt < retry.Count; attempt++ {
		if err := t.ready(req.Context()); err != nil {
			if current.Body != nil {
				current.Body.Close()
			}
			return nil, err
		}
		resp, err := t.base.RoundTrip(current)
		if err != nil {
			return resp, err
		}
		delay := retryAfter(resp.Header.Get("Retry-After"), t.now())
		limited := resp.StatusCode == 429 || resp.StatusCode == 529
		if limited {
			delay = max(delay, retry.Delay*time.Duration(1<<attempt))
		}
		if delay > 0 {
			t.mu.Lock()
			if until := t.now().Add(delay); until.After(t.resume) {
				t.resume = until
			}
			t.mu.Unlock()
		}
		if !limited {
			return resp, nil
		}
		if attempt == retry.Count-1 || (req.Body != nil && req.GetBody == nil) {
			apiErr := &notion.APIError{Status: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
			// Keep SDK error classification and the server's diagnostic available.
			var decoded notion.APIError
			if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&decoded) == nil {
				apiErr.Code, apiErr.Message, apiErr.Object = decoded.Code, decoded.Message, decoded.Object
			}
			resp.Body.Close()
			return nil, &ExhaustedError{Err: apiErr}
		}
		// Close the rejected response before waiting or replaying the request.
		io.Copy(io.Discard, io.LimitReader(resp.Body, 32<<10))
		resp.Body.Close()
		current = req.Clone(req.Context())
		if req.Body != nil {
			current.Body, err = req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("replay Notion request: %w", err)
			}
		}
	}
	panic("unreachable")
}

func (t *transport) ready(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		t.mu.Lock()
		delay := t.resume.Sub(t.now())
		t.mu.Unlock()
		if delay > 0 {
			if err := t.wait(ctx, delay); err != nil {
				return err
			}
			continue // Another response may have extended the shared cooldown.
		}
		if t.limiter != nil {
			if err := t.limiter.Wait(ctx); err != nil {
				return err
			}
		}
		t.mu.Lock()
		ready := !t.resume.After(t.now())
		t.mu.Unlock()
		if ready {
			return nil
		}
	}
}

func retryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	const maxDelay = time.Duration(1<<63 - 1)
	if seconds, err := strconv.ParseUint(value, 10, 64); err == nil {
		if seconds > uint64(maxDelay/time.Second) {
			return maxDelay
		}
		return time.Duration(seconds) * time.Second
	} else if errors.Is(err, strconv.ErrRange) && strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
		return maxDelay
	}
	if date, err := http.ParseTime(value); err == nil {
		return max(0, date.Sub(now))
	}
	return 0
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
