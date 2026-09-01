package main

import (
	"net/http"
	"net/http/httptest"
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
