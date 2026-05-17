package main

import (
	"path/filepath"
	"testing"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/transformer"
)

func TestExtractNotionPageIDFromFilename(t *testing.T) {
	id := "11111111222233334444555555555555"
	got := extractNotionPageID(filepath.Join("backup", id+".md"), nil)
	if got != id {
		t.Fatalf("expected %q, got %q", id, got)
	}
}

func TestExtractNotionPageIDFromFrontMatter(t *testing.T) {
	id := "aaaaaaaa111122223333444444444444"
	content := []byte("---\naliases: " + id + "\n---\n\n# Title\n")
	got := extractNotionPageID(filepath.Join("backup", "title.md"), content)
	if got != id {
		t.Fatalf("expected %q, got %q", id, got)
	}
}

func TestEqualIgnoringEOL(t *testing.T) {
	if !equalIgnoringEOL([]byte("a\r\nb\r\n"), []byte("a\nb\n")) {
		t.Fatal("expected CRLF and LF content to match")
	}
}

func TestStripExportDecorations(t *testing.T) {
	r := &ReverseUploader{ExporterConfig: ExporterConfig{Markdown: transformer.MarkdownConfig{TitleToH1: true}}}
	title, body := r.uploadBody([]byte("---\naliases: abc\n---\n\n# Page Title\n\nBody\n"))

	if title != "Page Title" {
		t.Fatalf("expected title, got %q", title)
	}
	if body != "Body\n" {
		t.Fatalf("expected body without frontmatter and h1, got %q", body)
	}
}

func TestMarkdownToNotionBlocks(t *testing.T) {
	blocks := markdownToNotionBlocks("# H1\n\n- item\n\n1. one\n\n- [x] done\n\n```go\nfmt.Println(1)\n```\n")
	if len(blocks) != 5 {
		t.Fatalf("expected 5 blocks, got %d", len(blocks))
	}
	if _, ok := blocks[0].(*notion.Heading1Block); !ok {
		t.Fatalf("expected heading block, got %T", blocks[0])
	}
	if _, ok := blocks[1].(*notion.BulletedListItemBlock); !ok {
		t.Fatalf("expected bullet block, got %T", blocks[1])
	}
	if _, ok := blocks[2].(*notion.NumberedListItemBlock); !ok {
		t.Fatalf("expected numbered block, got %T", blocks[2])
	}
	if _, ok := blocks[3].(*notion.ToDoBlock); !ok {
		t.Fatalf("expected todo block, got %T", blocks[3])
	}
	if _, ok := blocks[4].(*notion.CodeBlock); !ok {
		t.Fatalf("expected code block, got %T", blocks[4])
	}
}

func TestRichTextChunksKeepsUnicodeIntact(t *testing.T) {
	chunks := richTextChunks("中文")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Text.Content != "中文" {
		t.Fatalf("expected unicode content to remain intact, got %q", chunks[0].Text.Content)
	}
}
