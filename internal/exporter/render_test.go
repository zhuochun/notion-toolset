package exporter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/notiontest"
	"github.com/zhuochun/notion-toolset/notionread"
	"github.com/zhuochun/notion-toolset/transformer"
)

const renderPageJSON = `{"object":"page","id":"page","parent":{"type":"database_id","database_id":"db"},"properties":{"Name":{"id":"title","type":"title","title":[{"type":"text","plain_text":"标题","text":{"content":"标题"}}]}}}`
const renderBlocksJSON = `{"object":"list","results":[{"object":"block","id":"para","type":"paragraph","paragraph":{"rich_text":[{"type":"text","annotations":{},"plain_text":"Hello 世界","text":{"content":"Hello 世界"}}]}}],"has_more":false}`

func TestRenderSessionFailureReuseAndClose(t *testing.T) {
	var requests atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/pages/bad":
			w.WriteHeader(400)
			io.WriteString(w, `{"object":"error","status":400,"code":"validation_error","message":"bad page"}`)
		case "/v1/pages/page":
			io.WriteString(w, renderPageJSON)
		default:
			io.WriteString(w, renderBlocksJSON)
		}
	}))
	defer s.Close()
	reader := notionread.New(notiontest.Client(t, s.URL))
	cfg := config.ExporterConfig{ExportSpeed: 3, Markdown: transformer.MarkdownConfig{NoAlias: true, NoFrontMatters: true, NoMetadata: true, TitleToH1: true}}
	if _, err := NewRenderSession(nil, cfg, nil, nil); err == nil {
		t.Fatal("nil reader accepted")
	}
	for _, speed := range []float64{0, 4, math.NaN()} {
		invalid := cfg
		invalid.ExportSpeed = speed
		if _, err := NewRenderSession(reader, invalid, nil, nil); err == nil {
			t.Fatalf("speed %v accepted", speed)
		}
	}
	session, err := NewRenderSession(reader, cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, _, err := session.RenderPage(context.Background(), "bad"); err == nil {
		t.Fatal("read error hidden")
	}
	page, got, err := session.RenderPage(context.Background(), "page")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# 标题\n\nHello 世界\n\n" {
		t.Fatalf("markdown=%q", got)
	}
	snapshot, err := reader.BlockSnapshot(context.Background(), page.ID, notionread.BestEffort)
	if err != nil {
		t.Fatal(err)
	}
	var streamed bytes.Buffer
	renderMarkdown(&streamed, cfg.Markdown, &page, snapshot, nil)
	if !bytes.Equal(got, streamed.Bytes()) {
		t.Fatal("streamed and session output differ")
	}
	session.Close()
	session.Close()
	before := requests.Load()
	if _, _, err := session.RenderPage(context.Background(), "page"); err == nil {
		t.Fatal("render after close accepted")
	}
	if requests.Load() != before {
		t.Fatal("closed session performed I/O")
	}
	if _, err := reader.Page(context.Background(), "page"); err != nil {
		t.Fatalf("borrowed reader closed: %v", err)
	}
}

func TestRenderSessionCloseWaitsForActiveAsset(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	assetDone := false
	asset := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		io.WriteString(w, "asset bytes")
	}))
	defer asset.Close()
	defer func() {
		if !assetDone {
			close(release)
		}
	}()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/pages/") {
			io.WriteString(w, renderPageJSON)
		} else {
			fmt.Fprintf(w, `{"object":"list","results":[{"object":"block","id":"image","type":"image","image":{"type":"file","file":{"url":%q}}}],"has_more":false}`, asset.URL+"/image.png")
		}
	}))
	defer s.Close()
	cfg := config.ExporterConfig{ExportSpeed: 3, AssetDirectory: t.TempDir(), Markdown: transformer.MarkdownConfig{NoAlias: true, NoFrontMatters: true, NoMetadata: true}}
	session, err := NewRenderSession(notionread.New(notiontest.Client(t, s.URL)), cfg, asset.Client(), log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	rendered := make(chan error, 1)
	go func() { _, _, err := session.RenderPage(context.Background(), "page"); rendered <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("download did not start")
	}
	closed := make(chan struct{})
	go func() { session.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("Close returned before active asset")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	assetDone = true
	select {
	case err := <-rendered:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("render stuck")
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close stuck")
	}
	data, err := os.ReadFile(filepath.Join(cfg.AssetDirectory, "image.png"))
	if err != nil || string(data) != "asset bytes" {
		t.Fatalf("asset=%q err=%v", data, err)
	}
}

func TestExportRunCleanupFailureBoundaries(t *testing.T) {
	for _, mode := range []string{"full", "incremental", "one", "disabled", "scan-error", "worker-error"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			stale := filepath.Join(dir, strings.Repeat("a", 32)+".md")
			if err := os.WriteFile(stale, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if mode == "scan-error" || mode == "worker-error" && strings.Contains(r.URL.Path, "/blocks/") {
					w.WriteHeader(400)
					io.WriteString(w, `{"object":"error","status":400,"code":"validation_error","message":"fixture error"}`)
					return
				}
				if strings.Contains(r.URL.Path, "/query") {
					io.WriteString(w, `{"object":"list","results":[`+renderPageJSON+`],"has_more":false}`)
				} else if strings.Contains(r.URL.Path, "/pages/") {
					io.WriteString(w, renderPageJSON)
				} else {
					io.WriteString(w, renderBlocksJSON)
				}
			}))
			defer s.Close()
			e := Exporter{Client: notiontest.Client(t, s.URL), Logger: log.New(&logs, "", 0), ExporterConfig: config.ExporterConfig{DatabaseID: "db", Directory: dir, ExportSpeed: 3, CleanupDeleted: mode != "disabled", Markdown: transformer.MarkdownConfig{NoAlias: true, NoMetadata: true, NoFrontMatters: true}}}
			if mode == "incremental" {
				e.LookbackDays = 1
			}
			if mode == "one" {
				e.ExecOne = "page"
			}
			if err := e.Validate(); err != nil {
				t.Fatal(err)
			}
			err := e.Run()
			if (err != nil) != (mode == "scan-error") {
				t.Fatalf("Run=%v logs=%s", err, &logs)
			}
			_, statErr := os.Stat(stale)
			wantDeleted := mode == "full" || mode == "worker-error"
			if os.IsNotExist(statErr) != wantDeleted {
				t.Fatalf("stale stat=%v wantDeleted=%v", statErr, wantDeleted)
			}
			if mode == "worker-error" && !strings.Contains(logs.String(), "Failed to export") {
				t.Fatalf("missing worker failure log: %s", &logs)
			}
		})
	}
}
