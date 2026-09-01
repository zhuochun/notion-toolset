package transformer

import (
	"strings"
	"testing"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/notionread"
)

func TestMarkdownChildDatabaseUsesPlaceholderText(t *testing.T) {
	blocks := []notion.Block{
		&notion.ChildDatabaseBlock{
			Title: "Projects",
		},
	}
	m := New(MarkdownConfig{}, nil, notionread.NewSnapshot("root", blocks, nil), nil)

	output := m.Transform()

	if !strings.Contains(output, "> Child database: Projects") {
		t.Fatalf("expected child database placeholder, got %q", output)
	}

	if strings.Contains(output, "[[") {
		t.Fatalf("expected child database not to render as wiki link, got %q", output)
	}
}

func TestMarkdownChildDatabasePlainTextModeUsesPlainText(t *testing.T) {
	blocks := []notion.Block{
		&notion.ChildDatabaseBlock{
			Title: "Projects",
		},
	}
	m := New(MarkdownConfig{PlainText: true}, nil, notionread.NewSnapshot("root", blocks, nil), nil)

	output := m.Transform()

	if !strings.Contains(output, "Child database: Projects") {
		t.Fatalf("expected plain text child database output, got %q", output)
	}

	if strings.Contains(output, "> Child database: Projects") {
		t.Fatalf("expected plain text mode to avoid markdown placeholder, got %q", output)
	}
}
