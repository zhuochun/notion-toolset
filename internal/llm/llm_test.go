package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/internal/notiontest"
)

func TestScanDirectPagesReturnsMultipleErrorsWithoutDeadlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := notiontest.Client(t, server.URL)
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
