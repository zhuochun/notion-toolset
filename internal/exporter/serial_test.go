package exporter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/notiontest"
	"github.com/zhuochun/notion-toolset/notionread"
	"github.com/zhuochun/notion-toolset/transformer"
)

func TestRenderSessionSerializesOverlappingRenders(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var pageReads atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/pages/") {
			if pageReads.Add(1) == 1 {
				close(entered)
				<-release
			}
			io.WriteString(w, renderPageJSON)
		} else {
			io.WriteString(w, renderBlocksJSON)
		}
	}))
	defer s.Close()
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	session, err := NewRenderSession(notionread.New(notiontest.Client(t, s.URL)), config.ExporterConfig{ExportSpeed: 3, Markdown: transformer.MarkdownConfig{NoAlias: true, NoFrontMatters: true, NoMetadata: true}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	render := func() { _, _, err := session.RenderPage(context.Background(), "page"); done <- err }
	go render()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first render did not start")
	}
	started := make(chan struct{})
	go func() { close(started); render() }()
	<-started
	time.Sleep(30 * time.Millisecond)
	if pageReads.Load() != 1 {
		t.Error("second render performed I/O while first render was active")
	}
	close(release)
	released = true
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("render did not finish")
		}
	}
	session.Close()
	if pageReads.Load() != 2 {
		t.Fatalf("page reads=%d", pageReads.Load())
	}
}
