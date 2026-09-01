package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dstotijn/go-notion"
	"github.com/go-yaml/yaml"
)

type recordingCommand struct {
	calls       []string
	validateErr error
	runErr      error
}

func (c *recordingCommand) Validate() error {
	c.calls = append(c.calls, "validate")
	return c.validateErr
}

func (c *recordingCommand) Run() error {
	c.calls = append(c.calls, "run")
	return c.runErr
}

func TestNewCommandMapsEverySupportedCommand(t *testing.T) {
	opts := CommandOptions{
		ExecOne:     "page-id",
		DebugMode:   true,
		Mode:        "upload",
		Workspace:   "https://example.notion.site",
		ResolvePort: 19000,
	}
	tests := []struct {
		name string
		want Cmd
	}{
		{name: "daily-journal", want: &DailyJournal{}},
		{name: "weekly-journal", want: &WeeklyJournal{}},
		{name: "flashback", want: &Flashback{}},
		{name: "duplicate", want: &DuplicateChecker{}},
		{name: "collector", want: &Collector{}},
		{name: "export", want: &Exporter{}},
		{name: "upload", want: &ReverseUploader{}},
		{name: "llm", want: &LangModel{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts.Name = tt.name
			got, err := newCommand(nil, Config{}, opts)
			if err != nil {
				t.Fatalf("new command: %v", err)
			}
			if reflect.TypeOf(got) != reflect.TypeOf(tt.want) {
				t.Fatalf("expected %T, got %T", tt.want, got)
			}
		})
	}

	exporter, err := newCommand(nil, Config{}, CommandOptions{Name: "export", ExecOne: opts.ExecOne, DebugMode: true})
	if err != nil {
		t.Fatalf("new export command: %v", err)
	}
	if got := exporter.(*Exporter); got.ExecOne != opts.ExecOne || !got.DebugMode {
		t.Fatalf("export options not propagated: %+v", got)
	}

	uploader, err := newCommand(nil, Config{}, CommandOptions{
		Name:        "upload",
		Mode:        opts.Mode,
		Workspace:   opts.Workspace,
		ResolvePort: opts.ResolvePort,
		DebugMode:   true,
	})
	if err != nil {
		t.Fatalf("new upload command: %v", err)
	}
	gotUploader := uploader.(*ReverseUploader)
	if gotUploader.Mode != opts.Mode || gotUploader.Workspace != opts.Workspace || gotUploader.ResolvePort != opts.ResolvePort || !gotUploader.DebugMode {
		t.Fatalf("upload options not propagated: %+v", gotUploader)
	}
}

func TestNewCommandRejectsUnknownCommand(t *testing.T) {
	if _, err := newCommand(nil, Config{}, CommandOptions{Name: "unknown"}); err == nil {
		t.Fatal("expected unknown command error")
	}
}

func TestExecuteCommandValidatesBeforeRunAndPreservesErrors(t *testing.T) {
	validateErr := errors.New("invalid config")
	runErr := errors.New("run failed")
	tests := []struct {
		name        string
		cmd         *recordingCommand
		wantCalls   []string
		wantErr     error
		wantContext string
	}{
		{
			name:      "success",
			cmd:       &recordingCommand{},
			wantCalls: []string{"validate", "run"},
		},
		{
			name:        "validation failure stops before run",
			cmd:         &recordingCommand{validateErr: validateErr},
			wantCalls:   []string{"validate"},
			wantErr:     validateErr,
			wantContext: "cmd test validate failed: invalid config",
		},
		{
			name:        "run failure is returned",
			cmd:         &recordingCommand{runErr: runErr},
			wantCalls:   []string{"validate", "run"},
			wantErr:     runErr,
			wantContext: "cmd test error: run failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executeCommand("test", tt.cmd)
			if !reflect.DeepEqual(tt.cmd.calls, tt.wantCalls) {
				t.Fatalf("expected calls %v, got %v", tt.wantCalls, tt.cmd.calls)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if tt.wantContext != "" && err.Error() != tt.wantContext {
				t.Fatalf("expected error context %q, got %q", tt.wantContext, err)
			}
		})
	}
}

func TestExampleConfigsMatchSchemaAndRequiredContract(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("example", "configs"))
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
			path := filepath.Join("example", "configs", entry.Name())
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}

			var cfg Config
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

func validateExampleConfig(t *testing.T, name string, cfg Config) {
	t.Helper()

	switch name {
	case "collector.yaml":
		if err := (&Collector{CollectorConfig: cfg.Collector}).Validate(); err != nil {
			t.Fatal(err)
		}
		assertParagraphTemplate(t, "collector", cfg.Collector.CollectDumpTextBlock)
	case "duplicate.yaml":
		if err := (&DuplicateChecker{DuplicateCheckerConfig: cfg.DuplicateChecker}).Validate(); err != nil {
			t.Fatal(err)
		}
		assertParagraphTemplate(t, "duplicate", cfg.DuplicateChecker.DuplicateDumpTextBlock)
	case "flashback.yaml", "llm-flashback.yaml":
		if err := (&Flashback{FlashbackConfig: cfg.Flashback}).Validate(); err != nil {
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
		q := NewDatabaseQuery(nil, cfg.Exporter.DatabaseID)
		if err := q.SetQuery(cfg.Exporter.DatabaseQuery, QueryBuilder{Date: "2026-01-01"}); err != nil {
			t.Fatalf("export query: %v", err)
		}
	case "llm-summary.yml", "llm-summary-json.yml":
		if cfg.LLM.Prompt == "" {
			t.Fatal("LLM example requires prompt")
		}
		if cfg.LLM.RespJSON {
			w := NewAppendBlock(nil, "page-id")
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

	q := NewDatabaseQuery(nil, databaseID)
	if err := q.SetQuery(queryText, QueryBuilder{Date: "2026-01-01"}); err != nil {
		t.Fatalf("journal query: %v", err)
	}

	raw, err := Tmpl("journal properties", propertiesText, PageBuilder{
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
	w := NewAppendBlock(nil, "page-id")
	if err := w.AddParagraph(name, blockText, BlockBuilder{Date: "2026-01-01", PageID: "page-id"}); err != nil {
		t.Fatalf("%s paragraph: %v", name, err)
	}
}
