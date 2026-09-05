package config

import (
	"errors"
	"time"

	"github.com/zhuochun/notion-toolset/transformer"
)

var ErrRequired = errors.New("Config Missing")

type Config struct {
	Flashback        FlashbackConfig        `yaml:"flashback"`
	DailyJournal     DailyJournalConfig     `yaml:"dailyJournal"`
	WeeklyJournal    WeeklyJournalConfig    `yaml:"weeklyJournal"`
	DuplicateChecker DuplicateCheckerConfig `yaml:"duplicateChecker"`
	Collector        CollectorConfig        `yaml:"collector"`
	Exporter         ExporterConfig         `yaml:"exporter"`
	LLM              LangModelConfig        `yaml:"llm"`
}

type DailyJournalConfig struct {
	DatabaseID     string `yaml:"databaseID"`
	Limit          int    `yaml:"limit"`
	PageQuery      string `yaml:"pageQuery"`
	PageProperties string `yaml:"pageProperties"`
}

type WeeklyJournalConfig struct {
	DatabaseID     string `yaml:"databaseID"`
	Limit          int    `yaml:"limit"`
	PageQuery      string `yaml:"pageQuery"`
	PageProperties string `yaml:"pageProperties"`
}

type FlashbackConfig struct {
	DatabaseID         string    `yaml:"databaseID"`
	DatabaseQuery      string    `yaml:"databaseQuery"`
	OldestTimestamp    time.Time `yaml:"oldestTimestamp"`    // Format time.RFC3339 2006-01-02T15:04:05Z07:00
	FlashbackNum       int       `yaml:"flashbackNum"`       // Number of flashback entries
	FlashbackPageID    string    `yaml:"flashbackPageID"`    // Page to write the flashback
	FlashbackJournalID string    `yaml:"flashbackJournalID"` // Use daily journal database ID, this will overwrite FlashbackPageID
	FlashbackTextBlock string    `yaml:"flashbackTextBlock"` // Format https://pkg.go.dev/github.com/dstotijn/go-notion#ParagraphBlock
	FlashbackChainFile string    `yaml:"flashbackChainFile"` // Filename for chain with LLM cmd
}

type CollectorConfig struct {
	DatabaseID           string   `yaml:"databaseID"`
	DatabaseQuery        string   `yaml:"databaseQuery"`
	CollectionIDs        []string `yaml:"collectionIDs"`
	CollectDumpID        string   `yaml:"collectDumpID"`
	CollectDumpTextBlock string   `yaml:"collectDumpTextBlock"` // Format https://pkg.go.dev/github.com/dstotijn/go-notion#ParagraphBlock

}

type DuplicateCheckerConfig struct {
	DatabaseID    string `yaml:"databaseID"`
	DatabaseQuery string `yaml:"databaseQuery"`
	// CheckProperties specifies property names used to detect duplicates.
	// A page is considered a duplicate when any of the listed property
	// values matches another page's value (OR semantics). If the slice is
	// empty, page titles are used. Empty or nil property values are ignored.
	CheckProperties        []string `yaml:"checkProperties"`
	BrokenURLProperty      string   `yaml:"brokenURLproperty"`
	DuplicateDumpID        string   `yaml:"duplicateDumpID"`
	DuplicateDumpTextBlock string   `yaml:"duplicateDumpTextBlock"` // Format https://pkg.go.dev/github.com/dstotijn/go-notion#ParagraphBlock

}

type ExporterConfig struct {
	DatabaseID    string `yaml:"databaseID"`
	DatabaseQuery string `yaml:"databaseQuery"`
	// export related
	LookbackDays       int      `yaml:"lookbackDays"`   // leave this empty for full backup
	Directory          string   `yaml:"directory"`      // output directory
	AssetDirectory     string   `yaml:"assetDirectory"` // output directory for assets (images, etc)
	CleanupDeleted     bool     `yaml:"cleanupDeleted"`
	UseTitleAsFilename bool     `yaml:"useTitleAsFilename"`
	ReplaceTitle       []string `yaml:"replaceTitle"`
	// transformer
	Markdown transformer.MarkdownConfig `yaml:"markdown"`
	// tuning https://developers.notion.com/reference/request-limits
	ExportSpeed float64 `yaml:"exportSpeed"`
	// debug
	DebugLimit int  `yaml:"debugLimit"`
	DebugCache bool `yaml:"debugCache"`
}

type LangModelConfig struct {
	DatabaseID    string `yaml:"databaseID"`
	DatabaseQuery string `yaml:"databaseQuery"`
	LookbackDays  int    `yaml:"lookbackDays"` // additional date info
	// Read from a chain file instead of database, overwrite database configs above
	// chain file is supported in flashback
	ChainFile string `yaml:"chainFile"`
	// Scan all pages then run a single LLM with all page contents
	GroupExec bool `yaml:"groupExec"`
	// Write the combined result to today's journal database instead of a page ID
	GroupJournalID string `yaml:"groupJournalID"`
	// LLM config prompt message
	Prompt      string   `yaml:"prompt"`
	Model       string   `yaml:"model"`       // optional, default to GPT3-Turbo
	Temperature *float32 `yaml:"temperature"` // optional
	// LLM response format
	// - format follow https://pkg.go.dev/github.com/dstotijn/go-notion#ParagraphBlock
	// - when using JSON mode, always instruct the model to produce JSON via some message in the conversation
	RespJSON      bool   `yaml:"respJSON"`      // optional, default to false
	RespTextBlock string `yaml:"respTextBlock"` // mandatory for JSON model, else optional and default convert to paragraphs
	// Tuning https://developers.notion.com/reference/request-limits
	TaskSpeed float64 `yaml:"taskSpeed"` // optional
	// skip processing a pages if chars is <min or >max thresholds
	PageMinChars int `yaml:"pageMinChars"`
	PageMaxChars int `yaml:"pageMaxChars"`
}
