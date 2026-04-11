package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/transformer"
)

func TestDownloadAssetUnsupportedExtension(t *testing.T) {
	tmpDir := t.TempDir()
	e := &Exporter{ExporterConfig: ExporterConfig{AssetDirectory: tmpDir}}

	asset := transformer.NewAssetFuture("1", "http://example.com/file.txt")

	filename, err := e.downloadAsset(asset)
	if err == nil || !strings.Contains(err.Error(), "unsupported extension") {
		t.Fatalf("expected unsupported extension error, got %v", err)
	}
	if filename != "" {
		t.Fatalf("expected empty filename, got %v", filename)
	}
}

func TestDownloadAssetSupportedExtension(t *testing.T) {
	tmpDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))
	defer server.Close()

	e := &Exporter{ExporterConfig: ExporterConfig{AssetDirectory: tmpDir}}
	asset := transformer.NewAssetFuture("1", server.URL+"/img.png")

	filename, err := e.downloadAsset(asset)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.HasSuffix(filename, ".png") {
		t.Fatalf("expected png filename, got %v", filename)
	}
	if _, err := os.Stat(filename); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

func TestDownloadAssetSupportedExtensionPDF(t *testing.T) {
	tmpDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))
	defer server.Close()

	e := &Exporter{ExporterConfig: ExporterConfig{AssetDirectory: tmpDir}}
	asset := transformer.NewAssetFuture("1", server.URL+"/doc.pdf")

	filename, err := e.downloadAsset(asset)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !strings.HasSuffix(filename, ".pdf") {
		t.Fatalf("expected pdf filename, got %v", filename)
	}

	if _, err := os.Stat(filename); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

func TestGetExportFilenameSlugifiesTitle(t *testing.T) {
	tmpDir := t.TempDir()
	e := &Exporter{ExporterConfig: ExporterConfig{Directory: tmpDir, UseTitleAsFilename: true}}

	page := notion.Page{
		ID: "11111111-2222-3333-4444-555555555555",
		Properties: notion.PageProperties{
			Title: notion.PageTitle{
				Title: []notion.RichText{
					{PlainText: "Hello / World*"},
				},
			},
		},
	}

	filename := e.getExportFilename(page)
	expected := filepath.Join(tmpDir, "hello-world.md")

	if filename != expected {
		t.Fatalf("expected %q, got %q", expected, filename)
	}
}

func TestGetExportFilenameAppendsPageIDSuffixOnCollision(t *testing.T) {
	tmpDir := t.TempDir()
	e := &Exporter{ExporterConfig: ExporterConfig{Directory: tmpDir, UseTitleAsFilename: true}}

	pageOne := notion.Page{
		ID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		Properties: notion.PageProperties{
			Title: notion.PageTitle{
				Title: []notion.RichText{
					{PlainText: "Same Title"},
				},
			},
		},
	}
	pageTwo := notion.Page{
		ID: "ffffffff-1111-2222-3333-444444444444",
		Properties: notion.PageProperties{
			Title: notion.PageTitle{
				Title: []notion.RichText{
					{PlainText: "Same Title"},
				},
			},
		},
	}

	first := e.getExportFilename(pageOne)
	second := e.getExportFilename(pageTwo)

	expectedFirst := filepath.Join(tmpDir, "same-title.md")
	expectedSecond := filepath.Join(tmpDir, "same-title-ffffffff111122223333444444444444.md")

	if first != expectedFirst {
		t.Fatalf("expected %q, got %q", expectedFirst, first)
	}

	if second != expectedSecond {
		t.Fatalf("expected %q, got %q", expectedSecond, second)
	}
}

func TestGetExportFilenameAppendsPageIDSuffixForReservedWindowsName(t *testing.T) {
	tmpDir := t.TempDir()
	e := &Exporter{ExporterConfig: ExporterConfig{Directory: tmpDir, UseTitleAsFilename: true}}

	page := notion.Page{
		ID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		Properties: notion.PageProperties{
			Title: notion.PageTitle{
				Title: []notion.RichText{
					{PlainText: "CON"},
				},
			},
		},
	}

	filename := e.getExportFilename(page)
	expected := filepath.Join(tmpDir, "con-aaaaaaaabbbbccccddddeeeeeeeeeeee.md")

	if filename != expected {
		t.Fatalf("expected %q, got %q", expected, filename)
	}
}

func TestSlugifyTitleTruncatesAndCleans(t *testing.T) {
	longTitle := strings.Repeat("title ", 50) + `with invalid <>:"/\\|?*chars`

	slug := transformer.SlugifyTitle(longTitle, 40)

	if len([]rune(slug)) != 40 {
		t.Fatalf("expected slug length 40, got %d", len([]rune(slug)))
	}

	if strings.ContainsAny(slug, "<>:\"/\\|?*") {
		t.Fatalf("slug contains invalid characters: %q", slug)
	}
}

func TestSlugifyTitleTrimsTrailingWindowsInvalidCharacters(t *testing.T) {
	slug := transformer.SlugifyTitle("Title.   ", 40)

	if slug != "title" {
		t.Fatalf("expected %q, got %q", "title", slug)
	}
}
