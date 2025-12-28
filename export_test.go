package main

import (
	"net/http"
	"net/http/httptest"
	"os"
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
