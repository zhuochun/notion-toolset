package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestCleanupDeletedPagesRemovesStaleMarkdownOnFullExport(t *testing.T) {
	tmpDir := t.TempDir()
	liveFile := filepath.Join(tmpDir, "live.md")
	staleFile := filepath.Join(tmpDir, "stale.md")
	keepTextFile := filepath.Join(tmpDir, "note.txt")

	mustWriteFile(t, liveFile, "live")
	mustWriteFile(t, staleFile, "stale")
	mustWriteFile(t, keepTextFile, "keep")

	e := &Exporter{
		ExporterConfig: ExporterConfig{
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
		ExporterConfig: ExporterConfig{
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
		ExporterConfig: ExporterConfig{
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
		ExporterConfig: ExporterConfig{
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
		ExporterConfig: ExporterConfig{
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
