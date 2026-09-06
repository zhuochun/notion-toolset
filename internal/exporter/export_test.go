package exporter

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/transformer"
)

func TestDownloadAssetUnsupportedExtension(t *testing.T) {
	tmpDir := t.TempDir()
	e := assetDownloader{directory: tmpDir}

	asset := transformer.NewAssetFuture("1", "http://example.com/file.txt")

	filename, err := e.download(asset)
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

	e := assetDownloader{directory: tmpDir}
	asset := transformer.NewAssetFuture("1", server.URL+"/img.png")

	filename, err := e.download(asset)
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

	e := assetDownloader{directory: tmpDir}
	asset := transformer.NewAssetFuture("1", server.URL+"/doc.pdf")

	filename, err := e.download(asset)
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

func TestDownloadAssetHTTPResultAndExistingFileReuse(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.WriteHeader(status)
				_, _ = w.Write([]byte("image bytes"))
			}))
			defer server.Close()
			d := assetDownloader{directory: t.TempDir(), client: server.Client()}
			asset := transformer.NewAssetFuture("image-id", server.URL+"/image.png")
			_, err := d.download(asset)
			if (err != nil) != (status != http.StatusOK) {
				t.Fatalf("HTTP %d: %v", status, err)
			}
			// Existing behavior reuses even an empty file left by a failed download.
			// This characterizes compatibility; it does not promise asset recovery.
			filename, err := d.download(asset)
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(filename)
			want := "image bytes"
			if status != http.StatusOK {
				want = ""
			}
			if err != nil || string(content) != want || requests != 1 {
				t.Fatalf("cached content=%q err=%v requests=%d", content, err, requests)
			}
		})
	}
}

func TestCleanupDeletedPagesRemovesStaleMarkdownOnFullExport(t *testing.T) {
	tmpDir := t.TempDir()
	liveFile := filepath.Join(tmpDir, "live.md")
	staleFile := filepath.Join(tmpDir, "stale.md")
	keepTextFile := filepath.Join(tmpDir, "note.txt")

	mustWriteFile(t, liveFile, "live")
	mustWriteFile(t, staleFile, "stale")
	mustWriteFile(t, keepTextFile, "keep")

	e := &Exporter{
		ExporterConfig: config.ExporterConfig{
			Directory:      tmpDir,
			CleanupDeleted: true,
		},
		exportedFiles: map[string]struct{}{
			liveFile: {},
		},
	}

	if err := e.cleanupDeletedPages(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertExists(t, liveFile)
	assertNotExists(t, staleFile)
	assertExists(t, keepTextFile)
}

func TestCleanupDeletedPagesIgnoresSubdirectoriesAndHiddenMarkdown(t *testing.T) {
	tmpDir := t.TempDir()
	hiddenFile := filepath.Join(tmpDir, ".internal.md")
	subDir := filepath.Join(tmpDir, "nested")
	subFile := filepath.Join(subDir, "child.md")

	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	mustWriteFile(t, hiddenFile, "hidden")
	mustWriteFile(t, subFile, "nested")

	e := &Exporter{
		ExporterConfig: config.ExporterConfig{
			Directory:      tmpDir,
			CleanupDeleted: true,
		},
		exportedFiles: map[string]struct{}{},
	}

	if err := e.cleanupDeletedPages(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertExists(t, hiddenFile)
	assertExists(t, subFile)
}

func TestCleanupDeletedPagesSkipsIncrementalExport(t *testing.T) {
	tmpDir := t.TempDir()
	staleFile := filepath.Join(tmpDir, "stale.md")
	mustWriteFile(t, staleFile, "stale")

	e := &Exporter{
		ExporterConfig: config.ExporterConfig{
			Directory:      tmpDir,
			CleanupDeleted: true,
			LookbackDays:   1,
		},
		exportedFiles: map[string]struct{}{},
	}

	if err := e.cleanupDeletedPages(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertExists(t, staleFile)
}

func TestCleanupDeletedPagesSkipsExecOne(t *testing.T) {
	tmpDir := t.TempDir()
	staleFile := filepath.Join(tmpDir, "stale.md")
	mustWriteFile(t, staleFile, "stale")

	e := &Exporter{
		ExecOne: "page-id",
		ExporterConfig: config.ExporterConfig{
			Directory:      tmpDir,
			CleanupDeleted: true,
		},
		exportedFiles: map[string]struct{}{},
	}

	if err := e.cleanupDeletedPages(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertExists(t, staleFile)
}

func TestCleanupDeletedPagesDisabledPreservesFiles(t *testing.T) {
	tmpDir := t.TempDir()
	staleFile := filepath.Join(tmpDir, "stale.md")
	mustWriteFile(t, staleFile, "stale")

	e := &Exporter{
		ExporterConfig: config.ExporterConfig{
			Directory:      tmpDir,
			CleanupDeleted: false,
		},
		exportedFiles: map[string]struct{}{},
	}

	if err := e.cleanupDeletedPages(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	assertExists(t, staleFile)
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted: %v, stat err: %v", path, err)
	}
}

func TestGetExportFilenameSlugifiesTitle(t *testing.T) {
	tmpDir := t.TempDir()
	e := &Exporter{ExporterConfig: config.ExporterConfig{Directory: tmpDir, UseTitleAsFilename: true}}

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
	e := &Exporter{ExporterConfig: config.ExporterConfig{Directory: tmpDir, UseTitleAsFilename: true}}

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
	e := &Exporter{ExporterConfig: config.ExporterConfig{Directory: tmpDir, UseTitleAsFilename: true}}

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
