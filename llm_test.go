package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dstotijn/go-notion"
)

func TestScanDirectPagesReturnsMultipleErrorsWithoutDeadlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := newNotionTestClient(t, server.URL)
	m := &LangModel{Client: client}

	pageCount := 0
	err := m.scanDirectPages(context.Background(), []string{"bad-1", "bad-2"}, func(notion.Page) error {
		pageCount++
		return nil
	})
	if pageCount != 0 {
		t.Fatalf("expected no pages, got %d", pageCount)
	}
	if err == nil || !strings.Contains(err.Error(), "bad1") || !strings.Contains(err.Error(), "bad2") {
		t.Fatalf("expected both page errors, got %v", err)
	}
}

func TestSetFlashbackPageIDReturnsErrorWhenJournalMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","results":[],"next_cursor":null,"has_more":false,"type":"page_or_database","page_or_database":{}}`))
	}))
	defer server.Close()

	client := newNotionTestClient(t, server.URL)
	f := &Flashback{
		Client: client,
		FlashbackConfig: FlashbackConfig{
			FlashbackJournalID: "journal-db",
		},
	}

	if err := f.SetFlashbackPageID(); err == nil {
		t.Fatalf("expected missing journal error")
	}
}

func newNotionTestClient(t *testing.T, baseURL string) *notion.Client {
	t.Helper()

	target, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}

	httpClient := &http.Client{
		Transport: rewriteHostTransport{target: target},
	}

	return notion.NewClient("token", notion.WithHTTPClient(httpClient))
}

type rewriteHostTransport struct {
	target *url.URL
}

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.target.Scheme
	clone.URL.Host = t.target.Host
	clone.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(clone)
}
