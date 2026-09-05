package app

import (
	"errors"
	"log"
	"reflect"
	"testing"

	"github.com/zhuochun/notion-toolset/internal/collector"
	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/duplicate"
	"github.com/zhuochun/notion-toolset/internal/exporter"
	"github.com/zhuochun/notion-toolset/internal/flashback"
	"github.com/zhuochun/notion-toolset/internal/journal"
	"github.com/zhuochun/notion-toolset/internal/llm"
	"github.com/zhuochun/notion-toolset/internal/upload"
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
		{name: "daily-journal", want: &journal.DailyJournal{}},
		{name: "weekly-journal", want: &journal.WeeklyJournal{}},
		{name: "flashback", want: &flashback.Flashback{}},
		{name: "duplicate", want: &duplicate.DuplicateChecker{}},
		{name: "collector", want: &collector.Collector{}},
		{name: "export", want: &exporter.Exporter{}},
		{name: "upload", want: &upload.ReverseUploader{}},
		{name: "llm", want: &llm.LangModel{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts.Name = tt.name
			got, err := newCommand(Runtime{}, nil, config.Config{}, opts)
			if err != nil {
				t.Fatalf("new command: %v", err)
			}
			if reflect.TypeOf(got) != reflect.TypeOf(tt.want) {
				t.Fatalf("expected %T, got %T", tt.want, got)
			}
		})
	}

	exportCmd, err := newCommand(Runtime{}, nil, config.Config{}, CommandOptions{Name: "export", ExecOne: opts.ExecOne, DebugMode: true})
	if err != nil {
		t.Fatalf("new export command: %v", err)
	}
	if got := exportCmd.(*exporter.Exporter); got.ExecOne != opts.ExecOne || !got.DebugMode {
		t.Fatalf("export options not propagated: %+v", got)
	}

	uploader, err := newCommand(Runtime{}, nil, config.Config{}, CommandOptions{
		Name:        "upload",
		Mode:        opts.Mode,
		Workspace:   opts.Workspace,
		ResolvePort: opts.ResolvePort,
		DebugMode:   true,
	})
	if err != nil {
		t.Fatalf("new upload command: %v", err)
	}
	gotUploader := uploader.(*upload.ReverseUploader)
	if gotUploader.Mode != opts.Mode || gotUploader.Workspace != opts.Workspace || gotUploader.ResolvePort != opts.ResolvePort || !gotUploader.DebugMode {
		t.Fatalf("upload options not propagated: %+v", gotUploader)
	}
}

func TestNewCommandRejectsUnknownCommand(t *testing.T) {
	if _, err := newCommand(Runtime{}, nil, config.Config{}, CommandOptions{Name: "unknown"}); err == nil {
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
			err := executeCommand(log.Default(), "test", tt.cmd)
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
