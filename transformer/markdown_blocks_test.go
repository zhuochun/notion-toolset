package transformer

import (
	"strings"
	"testing"

	"github.com/dstotijn/go-notion"
)

func TestMarkdownChildDatabaseUsesPlaceholderText(t *testing.T) {
	m := New(MarkdownConfig{}, nil, []notion.Block{
		&notion.ChildDatabaseBlock{
			Title: "Projects",
		},
	}, nil, nil)

	output := m.Transform()

	if !strings.Contains(output, "> Child database: Projects") {
		t.Fatalf("expected child database placeholder, got %q", output)
	}

	if strings.Contains(output, "[[") {
		t.Fatalf("expected child database not to render as wiki link, got %q", output)
	}
}

func TestMarkdownChildDatabasePlainTextModeUsesPlainText(t *testing.T) {
	m := New(MarkdownConfig{PlainText: true}, nil, []notion.Block{
		&notion.ChildDatabaseBlock{
			Title: "Projects",
		},
	}, nil, nil)

	output := m.Transform()

	if !strings.Contains(output, "Child database: Projects") {
		t.Fatalf("expected plain text child database output, got %q", output)
	}

	if strings.Contains(output, "> Child database: Projects") {
		t.Fatalf("expected plain text mode to avoid markdown placeholder, got %q", output)
	}
}
