package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/dstotijn/go-notion"
	"github.com/go-yaml/yaml"
)

var (
	flagCmd        = flag.String("cmd", "", "Run command")
	flagExecOne    = flag.String("one", "", "Run with one ID") // special for some commands
	flagMulti      = flag.Bool("multi", false, "Multiple configs in the config file")
	flagMultiIdx   = flag.Int("idx", -1, "Use a specific config by index in the multiple config") // start from index 0
	flagRepeat     = flag.Int("repeat", 1, "Repeat this command")                                 // start with default 1 time
	flagConfigPath = flag.String("config", "", "Path to config file")
	flagDebugMode  = flag.Bool("debug", false, "Enable debug mode")
	flagMode       = flag.String("mode", "dry-run", "Command mode")
	flagWorkspace  = flag.String("workspace", "https://www.notion.so", "Notion workspace host or base URL for upload resolve mode")
	flagPort       = flag.Int("port", 17889, "Local port for upload resolve mode UI")
)

var (
	layoutDate  = "2006-01-02"                            // date format used in journal title
	hashIDRegex = regexp.MustCompile("([a-zA-Z0-9]{32})") // to extract the pageID
)

type Config struct {
	Flashback        FlashbackConfig        `yaml:"flashback"`
	DailyJournal     DailyJournalConfig     `yaml:"dailyJournal"`
	WeeklyJournal    WeeklyJournalConfig    `yaml:"weeklyJournal"`
	DuplicateChecker DuplicateCheckerConfig `yaml:"duplicateChecker"`
	Collector        CollectorConfig        `yaml:"collector"`
	Exporter         ExporterConfig         `yaml:"exporter"`
	LLM              LangModelConfig        `yaml:"llm"`
}

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

func main() {
	flag.Parse()

	if *flagExecOne != "" && strings.HasPrefix(*flagExecOne, "https:") {
		matches := hashIDRegex.FindStringSubmatch(*flagExecOne)
		// use the ID only
		if len(matches) > 1 {
			*flagExecOne = matches[1]

			log.Printf("Parsed ID: %v", *flagExecOne)
		}
	}

	notionClient := newNotionClient()
	commandOptions := CommandOptions{
		Name:        *flagCmd,
		ExecOne:     *flagExecOne,
		DebugMode:   *flagDebugMode,
		Mode:        *flagMode,
		Workspace:   *flagWorkspace,
		ResolvePort: *flagPort,
	}
	run := func(cfg Config) {
		if err := runCmd(notionClient, cfg, commandOptions); err != nil {
			log.Fatal(err)
		}
	}

	if *flagMulti {
		configs := loadMultiConfig(*flagConfigPath)
		if *flagDebugMode {
			log.Printf("MultiConfig len: %v", len(configs))
		}
		if *flagMultiIdx >= len(configs) {
			log.Fatalf("config index out of range: idx=%d, len=%d", *flagMultiIdx, len(configs))
		}

		repeat(func() {
			if *flagMultiIdx >= 0 {
				run(configs[*flagMultiIdx])
			} else {
				for _, cfg := range configs {
					run(cfg)
				}
			}
		}, *flagRepeat)
	} else {
		config := loadConfig(*flagConfigPath)

		repeat(func() {
			run(config)
		}, *flagRepeat)
	}
}

func repeat(do func(), times int) {
	for i := 0; i < times; i++ {
		do()
	}
}

func runCmd(notionClient *notion.Client, cfg Config, opts CommandOptions) error {
	if opts.DebugMode {
		log.Printf("Run cmd: %v, config: %+v", opts.Name, cfg)
	} else {
		log.Printf("Run cmd: %v", opts.Name)
	}

	cmd, err := newCommand(notionClient, cfg, opts)
	if err != nil {
		return err
	}
	return executeCommand(opts.Name, cmd)
}

func executeCommand(name string, cmd Cmd) error {
	if err := cmd.Validate(); err != nil {
		return fmt.Errorf("cmd %v validate failed: %w", name, err)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cmd %v error: %w", name, err)
	}

	log.Printf("cmd %v completed", name)
	return nil
}

func newCommand(notionClient *notion.Client, cfg Config, opts CommandOptions) (Cmd, error) {
	switch opts.Name {
	case "daily-journal": // create daily journal entries with title YYYY-MM-DD
		return &DailyJournal{
			DebugMode:          opts.DebugMode,
			Client:             notionClient,
			DailyJournalConfig: cfg.DailyJournal,
		}, nil
	case "weekly-journal": // create weekly journal entries with title like YYYY-MM-DD/YYYY-MM-DD
		return &WeeklyJournal{
			DebugMode:           opts.DebugMode,
			Client:              notionClient,
			WeeklyJournalConfig: cfg.WeeklyJournal,
		}, nil
	case "flashback": // get a random page from a database and resurface it
		return &Flashback{
			DebugMode:       opts.DebugMode,
			Client:          notionClient,
			FlashbackConfig: cfg.Flashback,
		}, nil
	case "duplicate": // find duplicated pages (same title) in a database
		return &DuplicateChecker{
			DebugMode:              opts.DebugMode,
			Client:                 notionClient,
			DuplicateCheckerConfig: cfg.DuplicateChecker,
		}, nil
	case "collector": // find certain pages from a database and dump the delta pages in a page
		return &Collector{
			DebugMode:       opts.DebugMode,
			Client:          notionClient,
			CollectorConfig: cfg.Collector,
		}, nil
	case "export": // export pages from a database into local folders in markdown
		return &Exporter{
			DebugMode:      opts.DebugMode,
			ExecOne:        opts.ExecOne,
			Client:         notionClient,
			ExporterConfig: cfg.Exporter,
		}, nil
	case "upload": // compare local uncommitted exports and upload changed content back to Notion
		return &ReverseUploader{
			DebugMode:      opts.DebugMode,
			Mode:           opts.Mode,
			Workspace:      opts.Workspace,
			ResolvePort:    opts.ResolvePort,
			Client:         notionClient,
			ExporterConfig: cfg.Exporter,
		}, nil
	case "llm": // run custom prompt on pages from a database
		return &LangModel{
			DebugMode:       opts.DebugMode,
			ExecOne:         opts.ExecOne,
			Client:          notionClient,
			LangModelConfig: cfg.LLM,
		}, nil
	default:
		return nil, fmt.Errorf("unknown cmd: `%v`", opts.Name)
	}
}

func newNotionClient() *notion.Client {
	notionToken := os.Getenv("NOTION_TOKEN")
	if notionToken == "" {
		log.Println("Empty Token in env.NOTION_TOKEN")
		os.Exit(1)
	}

	return notion.NewClient(notionToken)
}

func loadConfig(configPath string) Config {
	if configPath == "" && *flagExecOne != "" {
		return Config{} // allow empty config for execOne
	}

	configFile, err := os.ReadFile(configPath)
	if err != nil {
		log.Printf("Error in Config File (%v): %v", configPath, err)
		os.Exit(2)
	}

	config := Config{}
	err = yaml.Unmarshal(configFile, &config)
	if err != nil {
		log.Printf("Error in unmarshal Config: %v", err)
		os.Exit(2)
	}

	return config
}

func loadMultiConfig(configPath string) []Config {
	configFile, err := os.ReadFile(configPath)
	if err != nil {
		log.Printf("Error in Config File (%v): %v", configPath, err)
		os.Exit(2)
	}

	configs := []Config{}
	err = yaml.Unmarshal(configFile, &configs)
	if err != nil {
		log.Printf("Error in unmarshal Config: %v", err)
		os.Exit(2)
	}

	return configs
}
