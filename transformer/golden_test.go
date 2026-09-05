package transformer_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/notionread"
	"github.com/zhuochun/notion-toolset/transformer"
)

func TestNestedMarkdownGolden(t *testing.T) {
	var fixture struct {
		Page     notion.Page
		Roots    notion.BlockChildrenResponse
		Children notion.BlockChildrenResponse
	}
	data, err := os.ReadFile("testdata/nested.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("testdata/nested.md")
	if err != nil {
		t.Fatal(err)
	}
	// Git may check out fixture text with CRLF. Rendered output itself must stay LF.
	expected = bytes.ReplaceAll(expected, []byte("\r\n"), []byte("\n"))
	aliases := t.TempDir()
	if err := os.WriteFile(filepath.Join(aliases, "Linked 笔记.md"), []byte("---\r\naliases: linkedid\r\n---\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, failedAsset := range []bool{false, true} {
		assets := make(chan *transformer.AssetFuture)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for asset := range assets {
				if failedAsset {
					asset.Write("", errors.New("fixture download error"))
				} else {
					asset.Write("assets/image.png", nil)
				}
			}
		}()
		cfg := transformer.MarkdownConfig{IndexAlias: aliases, TitleToH1: true, NoMetadata: true}
		snapshot := notionread.NewSnapshot("page-id", fixture.Roots.Results, map[string][]notion.Block{"list": fixture.Children.Results})
		got := transformer.New(cfg, &fixture.Page, snapshot, assets).Transform()
		close(assets)
		<-done
		want := string(expected)
		if failedAsset {
			want = strings.ReplaceAll(want, "assets/image.png", "https://example.invalid/image.png")
		}
		if got != want {
			t.Fatalf("asset failure=%v\ngot=%q\nwant=%q", failedAsset, got, want)
		}
	}
}
