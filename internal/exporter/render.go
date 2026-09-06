package exporter

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"sync"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/notionread"
	"github.com/zhuochun/notion-toolset/transformer"
)

// RenderSession owns comparison rendering and its download workers. The supplied
// reader is borrowed and shared with upload's fresh pre-write reads.
type RenderSession struct {
	mu       sync.Mutex
	closed   bool
	reader   *notionread.Reader
	markdown transformer.MarkdownConfig
	assets   chan *transformer.AssetFuture
	workers  sync.WaitGroup
}

func NewRenderSession(reader *notionread.Reader, cfg config.ExporterConfig, client *http.Client, logger *log.Logger) (*RenderSession, error) {
	if reader == nil {
		return nil, errors.New("render session requires a reader")
	}
	if cfg.ExportSpeed < 1 || cfg.ExportSpeed > 3 || math.IsNaN(cfg.ExportSpeed) {
		return nil, errors.New("render session requires normalized exportSpeed (1 to 3)")
	}
	s := &RenderSession{reader: reader, markdown: cfg.Markdown}
	downloads := assetDownloader{directory: cfg.AssetDirectory, client: client, logger: logger}
	s.assets = downloads.start(&s.workers, int(cfg.ExportSpeed)*2)
	return s, nil
}

// RenderPage is synchronous and serialized with Close. Read failures are returned;
// existing Markdown asset fallback behavior remains owned by transformer.
func (s *RenderSession) RenderPage(ctx context.Context, pageID string) (notion.Page, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return notion.Page{}, nil, errors.New("render session is closed")
	}
	page, err := s.reader.Page(ctx, pageID)
	if err != nil {
		return notion.Page{}, nil, err
	}
	snapshot, err := s.reader.BlockSnapshot(ctx, page.ID, notionread.BestEffort)
	if err != nil {
		return notion.Page{}, nil, err
	}
	var out bytes.Buffer
	renderMarkdown(&out, s.markdown, &page, snapshot, s.assets)
	return page, out.Bytes(), nil
}

// Close waits for any active render and drains downloads. Repeated calls are safe.
// It neither closes the borrowed reader/client nor changes completed results.
func (s *RenderSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	close(s.assets)
	s.workers.Wait()
	s.closed = true
}

func renderMarkdown(out io.StringWriter, cfg transformer.MarkdownConfig, page *notion.Page, snapshot notionread.BlockSnapshot, assets chan *transformer.AssetFuture) {
	transformer.New(cfg, page, snapshot, assets).TransformOut(out)
}
