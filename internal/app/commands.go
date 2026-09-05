package app

import (
	"fmt"
	"log"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/internal/collector"
	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/duplicate"
	"github.com/zhuochun/notion-toolset/internal/exporter"
	"github.com/zhuochun/notion-toolset/internal/flashback"
	"github.com/zhuochun/notion-toolset/internal/journal"
	"github.com/zhuochun/notion-toolset/internal/llm"
	"github.com/zhuochun/notion-toolset/internal/upload"
)

type Cmd interface {
	// Validate check the config are correct
	Validate() error
	// Run the cmd
	Run() error
}

type CommandOptions struct {
	Name        string
	ExecOne     string
	DebugMode   bool
	Mode        string
	Workspace   string
	ResolvePort int
}

func runCmd(rt Runtime, notionClient *notion.Client, cfg config.Config, opts CommandOptions) error {
	if opts.DebugMode {
		rt.logger().Printf("Run cmd: %v, config: %+v", opts.Name, cfg)
	} else {
		rt.logger().Printf("Run cmd: %v", opts.Name)
	}

	cmd, err := newCommand(rt, notionClient, cfg, opts)
	if err != nil {
		return err
	}
	return executeCommand(rt.logger(), opts.Name, cmd)
}

func executeCommand(logger *log.Logger, name string, cmd Cmd) error {
	if err := cmd.Validate(); err != nil {
		return fmt.Errorf("cmd %v validate failed: %w", name, err)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cmd %v error: %w", name, err)
	}

	logger.Printf("cmd %v completed", name)
	return nil
}

func newCommand(rt Runtime, notionClient *notion.Client, cfg config.Config, opts CommandOptions) (Cmd, error) {
	switch opts.Name {
	case "daily-journal":
		return &journal.DailyJournal{
			Logger:             rt.logger(),
			DebugMode:          opts.DebugMode,
			Client:             notionClient,
			DailyJournalConfig: cfg.DailyJournal,
		}, nil
	case "weekly-journal":
		return &journal.WeeklyJournal{
			Logger:              rt.logger(),
			DebugMode:           opts.DebugMode,
			Client:              notionClient,
			WeeklyJournalConfig: cfg.WeeklyJournal,
		}, nil
	case "flashback":
		return &flashback.Flashback{
			Logger:          rt.logger(),
			DebugMode:       opts.DebugMode,
			Client:          notionClient,
			FlashbackConfig: cfg.Flashback,
		}, nil
	case "duplicate":
		return &duplicate.DuplicateChecker{
			Logger:                 rt.logger(),
			DebugMode:              opts.DebugMode,
			Client:                 notionClient,
			DuplicateCheckerConfig: cfg.DuplicateChecker,
		}, nil
	case "collector":
		return &collector.Collector{
			Logger:          rt.logger(),
			DebugMode:       opts.DebugMode,
			Client:          notionClient,
			CollectorConfig: cfg.Collector,
		}, nil
	case "export":
		return &exporter.Exporter{
			Logger:         rt.logger(),
			DebugMode:      opts.DebugMode,
			ExecOne:        opts.ExecOne,
			Client:         notionClient,
			ExporterConfig: cfg.Exporter,
		}, nil
	case "upload":
		return &upload.ReverseUploader{
			NewReader:      readerFactory(notionClient),
			Logger:         rt.logger(),
			DebugMode:      opts.DebugMode,
			Mode:           opts.Mode,
			Workspace:      opts.Workspace,
			ResolvePort:    opts.ResolvePort,
			Client:         notionClient,
			ExporterConfig: cfg.Exporter,
		}, nil
	case "llm":
		return &llm.LangModel{
			SetupClient:     rt.llmClient,
			Logger:          rt.logger(),
			DebugMode:       opts.DebugMode,
			ExecOne:         opts.ExecOne,
			Client:          notionClient,
			LangModelConfig: cfg.LLM,
		}, nil
	default:
		return nil, fmt.Errorf("unknown cmd: `%v`", opts.Name)
	}
}
