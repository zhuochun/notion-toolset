package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type runtimeTransportFunc func(*http.Request) (*http.Response, error)

func (f runtimeTransportFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestDefaultNotionClientHonorsServerCooldown(t *testing.T) {
	// Exercise production wiring without a live Notion request or credentials.
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	calls := 0
	http.DefaultTransport = runtimeTransportFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"30"}},
			Body: io.NopCloser(strings.NewReader(`{"object":"error","status":429,"code":"rate_limited","message":"slow down"}`))}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := (Runtime{}).notionClient("fixture").FindBlockChildrenByID(ctx, "root", nil)
	if calls != 1 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("default client did not wait for cooldown: calls=%d err=%v", calls, err)
	}
}
