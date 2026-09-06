package notionhttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/retry"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

func response(status int, header string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{header}},
		Body: &trackedBody{Reader: strings.NewReader(`{"object":"error","status":429,"code":"rate_limited","message":"slow down"}`)}}
}

func fakeTransport(base http.RoundTripper) (*transport, *[]time.Duration) {
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	waits := []time.Duration{}
	return &transport{base: base, now: func() time.Time { return now },
		wait: func(ctx context.Context, delay time.Duration) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			waits = append(waits, delay)
			now = now.Add(delay)
			return nil
		}}, &waits
}

func TestRetryAfterParsing(t *testing.T) {
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		header string
		want   time.Duration
	}{
		{"30", 30 * time.Second}, {" 30 ", 30 * time.Second}, {"0", 0},
		{"", 0}, {"-1", 0}, {"garbage", 0}, {"1.5", 0},
		{now.Add(45 * time.Second).Format(http.TimeFormat), 45 * time.Second},
		{now.Add(-time.Second).Format(http.TimeFormat), 0},
		{"9223372036854775807", time.Duration(1<<63 - 1)},
		{"999999999999999999999999999999999", time.Duration(1<<63 - 1)},
	} {
		t.Run(tt.header, func(t *testing.T) {
			if got := retryAfter(tt.header, now); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCooldownReplaysReadsAndWritesWithoutChangingPayload(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			var rejected *trackedBody
			tr, waits := fakeTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				body, err := io.ReadAll(req.Body)
				req.Body.Close()
				if err != nil || string(body) != `{"children":[{"text":"exact payload"}]}` || req.Method != method || req.Header.Get("Authorization") != "Bearer fixture" {
					t.Fatalf("request changed on attempt %d: method=%s body=%s err=%v", calls, req.Method, body, err)
				}
				if calls == 1 {
					resp := response(429, "30")
					rejected = resp.Body.(*trackedBody)
					return resp, nil
				}
				if !rejected.closed {
					t.Fatal("rejected response body remained open during retry")
				}
				return response(200, ""), nil
			}))
			req, _ := http.NewRequest(method, "https://api.notion.com/v1/fixture", strings.NewReader(`{"children":[{"text":"exact payload"}]}`))
			req.Header.Set("Authorization", "Bearer fixture")
			resp, err := (&http.Client{Transport: tr}).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if calls != 2 || !reflect.DeepEqual(*waits, []time.Duration{30 * time.Second}) {
				t.Fatalf("calls=%d waits=%v", calls, *waits)
			}
		})
	}
}

func TestExhaustionPreservesNotionErrorAndBoundedAttempts(t *testing.T) {
	calls := 0
	tr, waits := fakeTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return response(429, "30"), nil
	}))
	client := notion.NewClient("fixture", notion.WithHTTPClient(&http.Client{Transport: tr}))
	_, err := client.FindBlockChildrenByID(context.Background(), "root", nil)
	var exhausted *ExhaustedError
	var apiErr *notion.APIError
	if !errors.As(err, &exhausted) || !errors.As(err, &apiErr) || !errors.Is(err, notion.ErrRateLimited) || apiErr.Message != "slow down" {
		t.Fatalf("lost Notion error or exhaustion marker: %v", err)
	}
	if calls != retry.Count || !reflect.DeepEqual(*waits, []time.Duration{30 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second}) {
		t.Fatalf("calls=%d waits=%v", calls, *waits)
	}
}

func TestCooldownSharedWithNextRequestAndServerErrorsAreNotReplayed(t *testing.T) {
	calls := 0
	tr, waits := fakeTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response(503, "Sun, 06 Sep 2026 00:00:45 GMT"), nil
		}
		return response(200, ""), nil
	}))
	for range 2 {
		req, _ := http.NewRequest(http.MethodPost, "https://api.notion.com/v1/pages", strings.NewReader("{}"))
		resp, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if calls != 2 || !reflect.DeepEqual(*waits, []time.Duration{45 * time.Second}) {
		t.Fatalf("unexpected write replay or missing shared cooldown: calls=%d waits=%v", calls, *waits)
	}
}

func TestRateLimitFallbackAndOverload(t *testing.T) {
	for _, header := range []string{"", "invalid", "-1", "0"} {
		t.Run(header, func(t *testing.T) {
			calls := 0
			tr, waits := fakeTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls < 5 {
					return response(529, header), nil
				}
				return response(200, ""), nil
			}))
			req, _ := http.NewRequest(http.MethodGet, "https://api.notion.com/v1/fixture", nil)
			resp, err := tr.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if !reflect.DeepEqual(*waits, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}) {
				t.Fatalf("waits=%v", *waits)
			}
		})
	}
}

func TestCancellationDuringCooldownDoesNotSendRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	tr, _ := fakeTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return response(429, "30"), nil
	}))
	tr.wait = func(ctx context.Context, delay time.Duration) error {
		cancel()
		return waitContext(ctx, delay)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.notion.com/v1/fixture", nil)
	_, err := tr.RoundTrip(req)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestNonReplayableBodyAndNetworkErrorsAreNotRetried(t *testing.T) {
	for _, networkError := range []bool{false, true} {
		calls := 0
		tr, _ := fakeTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			req.Body.Close()
			if networkError {
				return nil, io.ErrUnexpectedEOF
			}
			return response(429, "30"), nil
		}))
		req, _ := http.NewRequest(http.MethodPost, "https://api.notion.com/v1/pages", io.NopCloser(strings.NewReader("{}")))
		_, err := tr.RoundTrip(req)
		if err == nil || calls != 1 {
			t.Fatalf("non-replayable request retried: calls=%d err=%v", calls, err)
		}
	}
}

func TestProductionTransportPacesRequests(t *testing.T) {
	tr := NewTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) { return response(200, ""), nil }))
	start := time.Now()
	for range 3 {
		req, _ := http.NewRequest(http.MethodGet, "https://api.notion.com/v1/fixture", nil)
		resp, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if elapsed := time.Since(start); elapsed < 2*time.Second/3 {
		t.Fatalf("requests were not paced: elapsed=%v", elapsed)
	}
}

func TestSDKAppendReplaysExactBatchAfterCooldown(t *testing.T) {
	var payloads []string
	tr, waits := fakeTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		payloads = append(payloads, string(body))
		if req.Method != http.MethodPatch || req.URL.Path != "/v1/blocks/dump/children" {
			t.Fatalf("unexpected append request: %s %s", req.Method, req.URL.Path)
		}
		if len(payloads) == 1 {
			return response(429, "30"), nil
		}
		resp := response(200, "")
		resp.Body = io.NopCloser(strings.NewReader(`{"object":"list","results":[],"has_more":false}`))
		return resp, nil
	}))
	client := notion.NewClient("fixture", notion.WithHTTPClient(&http.Client{Transport: tr}))
	_, err := client.AppendBlockChildren(context.Background(), "dump", []notion.Block{
		&notion.ParagraphBlock{RichText: []notion.RichText{{Type: notion.RichTextTypeText, Text: &notion.Text{Content: "preserve this reference"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 2 || payloads[0] != payloads[1] || !strings.Contains(payloads[0], "preserve this reference") || !reflect.DeepEqual(*waits, []time.Duration{30 * time.Second}) {
		t.Fatalf("payloads=%v waits=%v", payloads, *waits)
	}
}

func TestConcurrentRequestsRespectSharedCooldownAndCancellation(t *testing.T) {
	var calls atomic.Int32
	tr := NewTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return response(503, "30"), nil
	})).(*transport)
	tr.limiter = nil // Isolate server cooldown from proactive pacing.
	req, _ := http.NewRequest(http.MethodGet, "https://api.notion.com/v1/first", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	waiting := make(chan struct{}, 4)
	tr.wait = func(ctx context.Context, delay time.Duration) error {
		waiting <- struct{}{}
		return waitContext(ctx, delay)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errorsCh := make(chan error, 4)
	for range 4 {
		go func() {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.notion.com/v1/next", nil)
			resp, err := tr.RoundTrip(req)
			if resp != nil {
				resp.Body.Close()
			}
			errorsCh <- err
		}()
	}
	for range 4 {
		select {
		case <-waiting:
		case <-ctx.Done():
			t.Fatal("requests did not enter shared cooldown")
		}
	}
	cancel()
	for range 4 {
		if err := <-errorsCh; !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled wait, got %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("requests escaped cooldown: %d", got)
	}
}

func TestWaitingRequestRechecksExtendedCooldown(t *testing.T) {
	tr, waits := fakeTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) { return response(200, ""), nil }))
	tr.resume = tr.now().Add(30 * time.Second)
	wait := tr.wait
	tr.wait = func(ctx context.Context, delay time.Duration) error {
		if len(*waits) == 0 {
			tr.mu.Lock()
			tr.resume = tr.resume.Add(20 * time.Second)
			tr.mu.Unlock()
		}
		return wait(ctx, delay)
	}
	req, _ := http.NewRequest(http.MethodGet, "https://api.notion.com/v1/next", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !reflect.DeepEqual(*waits, []time.Duration{30 * time.Second, 20 * time.Second}) {
		t.Fatalf("cooldown extension ignored: %v", *waits)
	}
}
