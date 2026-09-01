package notionread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dstotijn/go-notion"
	"golang.org/x/time/rate"
)

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "rate limited", err: fmt.Errorf("query: %w", notion.ErrRateLimited), want: true},
		{name: "conflict", err: notion.ErrConflict, want: true},
		{name: "internal server", err: notion.ErrInternalServer, want: true},
		{name: "service unavailable", err: notion.ErrServiceUnavailable, want: true},
		{name: "unknown server error", err: &notion.APIError{Status: 502}, want: true},
		{name: "validation", err: notion.ErrValidation, want: false},
		{name: "object not found", err: notion.ErrObjectNotFound, want: false},
		{name: "canceled", err: context.Canceled, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryable(tt.err); got != tt.want {
				t.Fatalf("expected retryable=%v, got %v", tt.want, got)
			}
		})
	}
}

func TestForEachDatabasePagePreservesOrderAndMaxResults(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(databaseResponse("page-1", "next", true)))
			return
		}
		_, _ = w.Write([]byte(databaseResponse("page-2", "", false)))
	}))
	defer server.Close()

	reader := New(testClient(t, server.URL))
	var ids []string
	err := reader.ForEachDatabasePage(context.Background(), DatabaseRead{
		DatabaseID: "db",
		MaxResults: 2,
	}, func(page notion.Page) error {
		ids = append(ids, page.ID)
		return nil
	})

	if err != nil {
		t.Fatalf("read database: %v", err)
	}
	if want := []string{"page-1", "page-2"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("expected page order %v, got %v", want, ids)
	}
	if requests != 2 {
		t.Fatalf("expected two requests, got %d", requests)
	}
}

func TestForEachDatabasePageStopsOnVisitorError(t *testing.T) {
	requests := 0
	sentinel := errors.New("stop")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(databaseResponse("page-1", "next", true)))
	}))
	defer server.Close()

	err := New(testClient(t, server.URL)).ForEachDatabasePage(context.Background(), DatabaseRead{
		DatabaseID: "db",
	}, func(notion.Page) error {
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("expected visitor error, got %v", err)
	}
	if requests != 1 {
		t.Fatalf("expected one request, got %d", requests)
	}
}

func TestForEachDatabasePageStopsCallbacksAfterCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","results":[
			{"object":"page","id":"page-1","created_time":"2021-05-14T09:15:00.000Z","last_edited_time":"2021-05-14T09:15:00.000Z","archived":false,"properties":{},"parent":{"type":"database_id","database_id":"db"},"url":""},
			{"object":"page","id":"page-2","created_time":"2021-05-14T09:15:00.000Z","last_edited_time":"2021-05-14T09:15:00.000Z","archived":false,"properties":{},"parent":{"type":"database_id","database_id":"db"},"url":""}
		],"next_cursor":null,"has_more":false,"type":"page_or_database","page_or_database":{}}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	visits := 0
	err := New(testClient(t, server.URL)).ForEachDatabasePage(ctx, DatabaseRead{DatabaseID: "db"}, func(notion.Page) error {
		visits++
		cancel()
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if visits != 1 {
		t.Fatalf("expected one callback before cancellation, got %d", visits)
	}
}

func TestForEachDatabasePageCapsNotionPageSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var query struct {
			PageSize int `json:"page_size"`
		}
		if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
			t.Errorf("decode query: %v", err)
		}
		if query.PageSize != 100 {
			t.Errorf("expected page size 100, got %d", query.PageSize)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","results":[],"next_cursor":null,"has_more":false,"type":"page_or_database","page_or_database":{}}`))
	}))
	defer server.Close()

	err := New(testClient(t, server.URL)).ForEachDatabasePage(context.Background(), DatabaseRead{
		DatabaseID: "db",
		MaxResults: 250,
	}, func(notion.Page) error { return nil })
	if err != nil {
		t.Fatalf("read database: %v", err)
	}
}

func TestReaderCancellationStopsLimiterWait(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	}))
	defer server.Close()

	limiter := rate.NewLimiter(rate.Every(time.Hour), 1)
	if !limiter.Allow() {
		t.Fatal("expected to consume initial limiter token")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := New(testClient(t, server.URL), WithLimiter(limiter)).Page(ctx, "page")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("expected no HTTP request, got %d", requests.Load())
	}
}

func TestBlockSnapshotBestEffortVisitsSharedChildOnce(t *testing.T) {
	var mu sync.Mutex
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/blocks/root/children":
			_, _ = w.Write([]byte(`{"object":"list","results":[
				{"object":"block","id":"shared","created_time":"2021-05-14T09:15:00.000Z","last_edited_time":"2021-05-14T09:15:00.000Z","has_children":true,"archived":false,"type":"paragraph","paragraph":{"rich_text":[]}},
				{"object":"block","id":"shared","created_time":"2021-05-14T09:15:00.000Z","last_edited_time":"2021-05-14T09:15:00.000Z","has_children":true,"archived":false,"type":"paragraph","paragraph":{"rich_text":[]}}
			],"next_cursor":null,"has_more":false}`))
		case "/v1/blocks/shared/children":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"object":"error","status":400,"code":"validation_error","message":"invalid"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	snapshot, err := New(testClient(t, server.URL)).BlockSnapshot(context.Background(), "root", BestEffort)
	if err != nil {
		t.Fatalf("best effort snapshot: %v", err)
	}
	if len(snapshot.Issues()) != 1 {
		t.Fatalf("expected one issue, got %d", len(snapshot.Issues()))
	}
	if _, err := snapshot.Children("shared"); err == nil {
		t.Fatal("expected recorded child error")
	}
	mu.Lock()
	defer mu.Unlock()
	if requests["/v1/blocks/shared/children"] != 1 {
		t.Fatalf("expected shared child to be requested once, got %d", requests["/v1/blocks/shared/children"])
	}
}

func TestBlockSnapshotBestEffortPreservesPartialPagination(t *testing.T) {
	var grandchildRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/blocks/root/children":
			_, _ = w.Write([]byte(blockList(`{"object":"block","id":"child","created_time":"2021-05-14T09:15:00.000Z","last_edited_time":"2021-05-14T09:15:00.000Z","has_children":true,"archived":false,"type":"paragraph","paragraph":{"rich_text":[]}}`, "", false)))
		case "/v1/blocks/child/children":
			if r.URL.Query().Get("start_cursor") == "next" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"object":"error","status":400,"code":"validation_error","message":"second page failed"}`))
				return
			}
			_, _ = w.Write([]byte(blockList(`{"object":"block","id":"grandchild","created_time":"2021-05-14T09:15:00.000Z","last_edited_time":"2021-05-14T09:15:00.000Z","has_children":true,"archived":false,"type":"paragraph","paragraph":{"rich_text":[]}}`, "next", true)))
		case "/v1/blocks/grandchild/children":
			grandchildRequests.Add(1)
			_, _ = w.Write([]byte(blockList("", "", false)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	snapshot, err := New(testClient(t, server.URL)).BlockSnapshot(context.Background(), "root", BestEffort)
	if err != nil {
		t.Fatalf("best effort snapshot: %v", err)
	}
	children, childErr := snapshot.Children("child")
	if childErr == nil {
		t.Fatal("expected partial child error")
	}
	if len(children) != 1 || children[0].ID() != "grandchild" {
		t.Fatalf("expected partial grandchild block, got %#v", children)
	}
	if grandchildRequests.Load() != 1 {
		t.Fatalf("expected partial descendants to continue traversal, got %d requests", grandchildRequests.Load())
	}
}

func TestBlockSnapshotStopsAtChildPageBoundary(t *testing.T) {
	var childPageRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/blocks/root/children":
			_, _ = w.Write([]byte(blockList(`{"object":"block","id":"child-page","created_time":"2021-05-14T09:15:00.000Z","last_edited_time":"2021-05-14T09:15:00.000Z","has_children":true,"archived":false,"type":"child_page","child_page":{"title":"Child"}}`, "", false)))
		case "/v1/blocks/child-page/children":
			childPageRequests.Add(1)
			_, _ = w.Write([]byte(blockList("", "", false)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if _, err := New(testClient(t, server.URL)).BlockSnapshot(context.Background(), "root", BestEffort); err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if childPageRequests.Load() != 0 {
		t.Fatalf("expected child page boundary not to be traversed, got %d requests", childPageRequests.Load())
	}
}

func TestBlockForestCrossesChildPageBoundary(t *testing.T) {
	var childPageRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/blocks/root/children":
			_, _ = w.Write([]byte(blockList(`{"object":"block","id":"child-page","created_time":"2021-05-14T09:15:00.000Z","last_edited_time":"2021-05-14T09:15:00.000Z","has_children":true,"archived":false,"type":"child_page","child_page":{"title":"Child"}}`, "", false)))
		case "/v1/blocks/child-page/children":
			childPageRequests.Add(1)
			_, _ = w.Write([]byte(blockList("", "", false)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	snapshot, err := New(testClient(t, server.URL)).BlockForest(context.Background(), []string{"root"}, BestEffort)
	if err != nil {
		t.Fatalf("build forest: %v", err)
	}
	if childPageRequests.Load() != 1 {
		t.Fatalf("expected child page boundary to be traversed once, got %d requests", childPageRequests.Load())
	}
	if _, err := snapshot.Children("child-page"); err != nil {
		t.Fatalf("expected child page children in forest: %v", err)
	}
}

func TestBlockSnapshotPropagatesNestedCancellation(t *testing.T) {
	childStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/blocks/root/children" {
			_, _ = w.Write([]byte(blockList(`{"object":"block","id":"child","created_time":"2021-05-14T09:15:00.000Z","last_edited_time":"2021-05-14T09:15:00.000Z","has_children":true,"archived":false,"type":"paragraph","paragraph":{"rich_text":[]}}`, "", false)))
			return
		}
		close(childStarted)
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := New(testClient(t, server.URL)).BlockSnapshot(ctx, "root", BestEffort)
		done <- err
	}()
	<-childStarted
	cancel()

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected nested cancellation, got %v", err)
	}
}

func TestBlockSnapshotStrictRejectsNestedFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/blocks/root/children" {
			_, _ = w.Write([]byte(`{"object":"list","results":[{"object":"block","id":"child","created_time":"2021-05-14T09:15:00.000Z","last_edited_time":"2021-05-14T09:15:00.000Z","has_children":true,"archived":false,"type":"paragraph","paragraph":{"rich_text":[]}}],"next_cursor":null,"has_more":false}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"object":"error","status":400,"code":"validation_error","message":"invalid"}`))
	}))
	defer server.Close()

	snapshot, err := New(testClient(t, server.URL)).BlockSnapshot(context.Background(), "root", Strict)
	if err == nil {
		t.Fatal("expected strict snapshot error")
	}
	if len(snapshot.Roots()) != 0 {
		t.Fatal("expected no partial strict snapshot")
	}
}

func TestBlockSnapshotLoadsSiblingBranchesConcurrently(t *testing.T) {
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/blocks/root/children" {
			_, _ = w.Write([]byte(`{"object":"list","results":[
				{"object":"block","id":"child-a","created_time":"2021-05-14T09:15:00.000Z","last_edited_time":"2021-05-14T09:15:00.000Z","has_children":true,"archived":false,"type":"paragraph","paragraph":{"rich_text":[]}},
				{"object":"block","id":"child-b","created_time":"2021-05-14T09:15:00.000Z","last_edited_time":"2021-05-14T09:15:00.000Z","has_children":true,"archived":false,"type":"paragraph","paragraph":{"rich_text":[]}}
			],"next_cursor":null,"has_more":false}`))
			return
		}

		current := inFlight.Add(1)
		for {
			maximum := maxInFlight.Load()
			if current <= maximum || maxInFlight.CompareAndSwap(maximum, current) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		inFlight.Add(-1)
		_, _ = w.Write([]byte(`{"object":"list","results":[],"next_cursor":null,"has_more":false}`))
	}))
	defer server.Close()

	reader := New(testClient(t, server.URL), WithConcurrency(2))
	if _, err := reader.BlockSnapshot(context.Background(), "root", Strict); err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if got := maxInFlight.Load(); got < 2 {
		t.Fatalf("expected sibling reads to overlap, max in flight was %d", got)
	}
}

func TestReaderLimitsBlockConcurrencyAcrossSnapshots(t *testing.T) {
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := inFlight.Add(1)
		for {
			maximum := maxInFlight.Load()
			if current <= maximum || maxInFlight.CompareAndSwap(maximum, current) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		inFlight.Add(-1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(blockList("", "", false)))
	}))
	defer server.Close()

	reader := New(testClient(t, server.URL), WithConcurrency(2))
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if _, err := reader.BlockSnapshot(context.Background(), fmt.Sprintf("root-%d", index), Strict); err != nil {
				t.Errorf("build snapshot: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if got := maxInFlight.Load(); got > 2 {
		t.Fatalf("expected global block concurrency <= 2, got %d", got)
	}
}

func blockList(block, cursor string, hasMore bool) string {
	results := "[]"
	if block != "" {
		results = "[" + block + "]"
	}
	nextCursor := "null"
	if cursor != "" {
		nextCursor = `"` + cursor + `"`
	}
	return `{"object":"list","results":` + results + `,"next_cursor":` + nextCursor + `,"has_more":` + fmtBool(hasMore) + `}`
}

func databaseResponse(pageID, cursor string, hasMore bool) string {
	nextCursor := "null"
	if cursor != "" {
		nextCursor = `"` + cursor + `"`
	}
	return `{"object":"list","results":[{"object":"page","id":"` + pageID + `","created_time":"2021-05-14T09:15:00.000Z","last_edited_time":"2021-05-14T09:15:00.000Z","archived":false,"properties":{},"parent":{"type":"database_id","database_id":"db"},"url":""}],"next_cursor":` + nextCursor + `,"has_more":` + fmtBool(hasMore) + `,"type":"page_or_database","page_or_database":{}}`
}

func fmtBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func testClient(t *testing.T, baseURL string) *notion.Client {
	t.Helper()
	target, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	return notion.NewClient("token", notion.WithHTTPClient(&http.Client{Transport: testTransport{target: target}}))
}

type testTransport struct {
	target *url.URL
}

func (transport testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = transport.target.Scheme
	clone.URL.Host = transport.target.Host
	clone.Host = transport.target.Host
	return http.DefaultTransport.RoundTrip(clone)
}
