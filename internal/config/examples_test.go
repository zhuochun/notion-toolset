package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dstotijn/go-notion"
	"github.com/go-yaml/yaml"
	"github.com/zhuochun/notion-toolset/internal/collector"
	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/duplicate"
	"github.com/zhuochun/notion-toolset/internal/flashback"
	"github.com/zhuochun/notion-toolset/internal/notionops"
)

func TestExampleConfigsMatchSchemaAndRequiredContract(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "example", "configs"))
	if err != nil {
		t.Fatalf("read example configs: %v", err)
	}

	checked := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		t.Run(entry.Name(), func(t *testing.T) {
			path := filepath.Join("..", "..", "example", "configs", entry.Name())
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}

			var cfg config.Config
			if err := yaml.UnmarshalStrict(content, &cfg); err != nil {
				t.Fatalf("strict config decode: %v", err)
			}
			validateExampleConfig(t, entry.Name(), cfg)
		})
		checked++
	}

	if checked == 0 {
		t.Fatal("expected example configs")
	}
}

func validateExampleConfig(t *testing.T, name string, cfg config.Config) {
	t.Helper()

	switch name {
	case "collector.yaml":
		if err := (&collector.Collector{CollectorConfig: cfg.Collector}).Validate(); err != nil {
			t.Fatal(err)
		}
		assertParagraphTemplate(t, "collector", cfg.Collector.CollectDumpTextBlock)
	case "duplicate.yaml":
		if err := (&duplicate.DuplicateChecker{DuplicateCheckerConfig: cfg.DuplicateChecker}).Validate(); err != nil {
			t.Fatal(err)
		}
		assertParagraphTemplate(t, "duplicate", cfg.DuplicateChecker.DuplicateDumpTextBlock)
	case "flashback.yaml", "llm-flashback.yaml":
		if err := (&flashback.Flashback{FlashbackConfig: cfg.Flashback}).Validate(); err != nil {
			t.Fatal(err)
		}
		assertParagraphTemplate(t, "flashback", cfg.Flashback.FlashbackTextBlock)
	case "journal-daily.yaml":
		assertJournalTemplates(t, cfg.DailyJournal.DatabaseID, cfg.DailyJournal.Limit, cfg.DailyJournal.PageQuery, cfg.DailyJournal.PageProperties)
	case "journal-weekly.yaml":
		assertJournalTemplates(t, cfg.WeeklyJournal.DatabaseID, cfg.WeeklyJournal.Limit, cfg.WeeklyJournal.PageQuery, cfg.WeeklyJournal.PageProperties)
	case "export.yaml":
		if cfg.Exporter.DatabaseID == "" || cfg.Exporter.Directory == "" {
			t.Fatal("export example requires databaseID and directory")
		}
		q := notionops.NewDatabaseQuery(nil, cfg.Exporter.DatabaseID)
		if err := q.SetQuery(cfg.Exporter.DatabaseQuery, notionops.QueryBuilder{Date: "2026-01-01"}); err != nil {
			t.Fatalf("export query: %v", err)
		}
	case "llm-summary.yml", "llm-summary-json.yml":
		if cfg.LLM.Prompt == "" {
			t.Fatal("LLM example requires prompt")
		}
		if cfg.LLM.RespJSON {
			w := notionops.NewAppendBlock(nil, "page-id")
			err := w.AddBlocks("LLM response", cfg.LLM.RespTextBlock, map[string]interface{}{
				"Summary":     "summary",
				"KeyPoints":   []string{"key point"},
				"Conclusions": []string{"conclusion"},
				"Frameworks":  []string{"framework"},
			})
			if err != nil {
				t.Fatalf("LLM response block: %v", err)
			}
		}
	default:
		t.Fatalf("example config has no smoke contract: %s", name)
	}
}

func assertJournalTemplates(t *testing.T, databaseID string, limit int, queryText, propertiesText string) {
	t.Helper()
	if databaseID == "" || limit <= 0 {
		t.Fatal("journal example requires databaseID and positive limit")
	}

	q := notionops.NewDatabaseQuery(nil, databaseID)
	if err := q.SetQuery(queryText, notionops.QueryBuilder{Date: "2026-01-01"}); err != nil {
		t.Fatalf("journal query: %v", err)
	}

	raw, err := notionops.Tmpl("journal properties", propertiesText, notionops.PageBuilder{
		Title:      "2026-01-01",
		Date:       "2026-01-01",
		DateEnd:    "2026-01-07",
		DatabaseID: databaseID,
	})
	if err != nil {
		t.Fatalf("journal properties template: %v", err)
	}
	var properties notion.DatabasePageProperties
	if err := json.Unmarshal(raw, &properties); err != nil {
		t.Fatalf("journal properties: %v", err)
	}
}

func assertParagraphTemplate(t *testing.T, name, blockText string) {
	t.Helper()
	w := notionops.NewAppendBlock(nil, "page-id")
	if err := w.AddParagraph(name, blockText, notionops.BlockBuilder{Date: "2026-01-01", PageID: "page-id"}); err != nil {
		t.Fatalf("%s paragraph: %v", name, err)
	}
}
