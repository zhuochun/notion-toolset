package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dstotijn/go-notion"
)

func TestReportDuplicatePageSkipsAlreadyReportedPages(t *testing.T) {
	d := &DuplicateChecker{}
	reported := map[string]struct{}{
		"page-1": {},
	}

	if err := d.reportDuplicatePage(reported, "page-1"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestBrokenURLCheckFallsBackToGetWhenHeadUnsupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	d := &DuplicateChecker{
		DuplicateCheckerConfig: DuplicateCheckerConfig{
			BrokenURLProperty: "Link",
		},
	}

	page := testPageWithURLProperty("Link", server.URL)
	if d.brokenURLCheck(page) {
		t.Fatalf("expected URL to be treated as healthy")
	}
}

func TestBrokenURLCheckTreatsFailedGetFallbackAsBroken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	d := &DuplicateChecker{
		DuplicateCheckerConfig: DuplicateCheckerConfig{
			BrokenURLProperty: "Link",
		},
	}

	page := testPageWithURLProperty("Link", server.URL)
	if !d.brokenURLCheck(page) {
		t.Fatalf("expected URL to be treated as broken")
	}
}

func testPageWithURLProperty(name, url string) notion.Page {
	return notion.Page{
		Properties: notion.DatabasePageProperties{
			name: notion.DatabasePageProperty{
				Type: notion.DBPropTypeURL,
				URL:  &url,
			},
		},
	}
}
