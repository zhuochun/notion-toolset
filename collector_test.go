package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCollectorGetCollectedStopsOnBlockReadError(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"object":"error","status":400,"code":"validation_error","message":"invalid request"}`))
	}))
	defer server.Close()

	collector := &Collector{
		Client: newNotionTestClient(t, server.URL),
		CollectorConfig: CollectorConfig{
			CollectionIDs: []string{"bad-block"},
		},
	}

	collected, err := collector.GetCollected()
	if err == nil {
		t.Fatal("expected block read error")
	}
	if collected != nil {
		t.Fatalf("expected no partial collected result, got %v", collected)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("expected permanent error to stop after 1 request, got %d", got)
	}
}

func TestCollectorGetCollectedVisitsEachBlockOnce(t *testing.T) {
	var mu sync.Mutex
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/blocks/root-a/children", "/v1/blocks/root-b/children":
			_, _ = w.Write([]byte(`{
				"object":"list",
				"results":[{
					"object":"block",
					"id":"shared-child",
					"created_time":"2021-05-14T09:15:00.000Z",
					"last_edited_time":"2021-05-14T09:15:00.000Z",
					"has_children":true,
					"archived":false,
					"type":"paragraph",
					"paragraph":{"rich_text":[]}
				}],
				"next_cursor":null,
				"has_more":false
			}`))
		case "/v1/blocks/shared-child/children":
			_, _ = w.Write([]byte(`{"object":"list","results":[],"next_cursor":null,"has_more":false}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	collector := &Collector{
		Client: newNotionTestClient(t, server.URL),
		CollectorConfig: CollectorConfig{
			CollectionIDs: []string{"root-a", "root-b", "root-a"},
		},
	}

	if _, err := collector.GetCollected(); err != nil {
		t.Fatalf("get collected: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, path := range []string{
		"/v1/blocks/root-a/children",
		"/v1/blocks/root-b/children",
		"/v1/blocks/shared-child/children",
	} {
		if got := requests[path]; got != 1 {
			t.Fatalf("expected %s to be visited once, got %d requests", path, got)
		}
	}
}

func TestCollectorWriteBlocksBatchesAndContinuesAfterFailure(t *testing.T) {
	var requestNum atomic.Int32
	var mu sync.Mutex
	batchSizes := []int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/blocks/dump/children" {
			http.NotFound(w, r)
			return
		}

		var body struct {
			Children []json.RawMessage `json:"children"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		mu.Lock()
		batchSizes = append(batchSizes, len(body.Children))
		mu.Unlock()

		currentRequest := requestNum.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if currentRequest == 2 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"object":"error","status":500,"code":"internal_server_error","message":"failed"}`))
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","results":[],"next_cursor":null,"has_more":false}`))
	}))
	defer server.Close()

	pageIDs := make([]string, 201)
	for i := range pageIDs {
		pageIDs[i] = fmt.Sprintf("page-%03d", i)
	}
	collector := &Collector{
		Client: newNotionTestClient(t, server.URL),
		CollectorConfig: CollectorConfig{
			CollectDumpID: "dump",
			CollectDumpTextBlock: `{
				"rich_text":[{
					"type":"mention",
					"mention":{"type":"page","page":{"id":"{{.PageID}}"}}
				}]
			}`,
		},
	}

	succeeded, failed := collector.WriteBlocks(pageIDs)
	if succeeded != 101 || failed != 100 {
		t.Fatalf("expected 101 succeeded and 100 failed, got %d succeeded and %d failed", succeeded, failed)
	}
	if got := requestNum.Load(); got != 3 {
		t.Fatalf("expected 3 batch requests, got %d", got)
	}
	mu.Lock()
	gotBatchSizes := append([]int(nil), batchSizes...)
	mu.Unlock()
	wantBatchSizes := []int{100, 100, 1}
	for i, want := range wantBatchSizes {
		if gotBatchSizes[i] != want {
			t.Fatalf("batch %d: expected %d blocks, got %d", i, want, gotBatchSizes[i])
		}
	}
}

func TestCollectorWriteBlocksCountsBuildFailureWithoutReturningAnError(t *testing.T) {
	collector := &Collector{
		CollectorConfig: CollectorConfig{
			CollectDumpTextBlock: `{`,
		},
	}

	succeeded, failed := collector.WriteBlocks([]string{"page-1", "page-2"})
	if succeeded != 0 || failed != 2 {
		t.Fatalf("expected 0 succeeded and 2 failed, got %d succeeded and %d failed", succeeded, failed)
	}
}
