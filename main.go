package main

import (
	"flag"
	"log"
	"os"

	"github.com/dstotijn/go-notion"
	"github.com/go-yaml/yaml"
)

const (
	layoutDate = "2006-01-02"
)

var (
	flagCmd        = flag.String("cmd", "", "Run command")
	flagMulti      = flag.Bool("multi", false, "Multiple configs in the config file")
	flagMultiIdx   = flag.Int("idx", -1, "Use a specific config by index in the multiple config") // start from index 0
	flagRepeat     = flag.Int("repeat", 1, "Repeat this command")                                 // start with default 1 time
	flagConfigPath = flag.String("config", "", "Path to config file")
	flagDebugMode  = flag.Bool("debug", false, "Enable debug mode")
)

type Config struct {
	Flashback        FlashbackConfig        `yaml:"flashback"`
	DailyJournal     DailyJournalConfig     `yaml:"dailyJournal"`
	WeeklyJournal    WeeklyJournalConfig    `yaml:"weeklyJournal"`
	DuplicateChecker DuplicateCheckerConfig `yaml:"duplicateChecker"`
	Collector        CollectorConfig        `yaml:"collector"`
	Exporter         ExporterConfig         `yaml:"exporter"`
}

type Cmd interface {
	// Validate check the config are correct
	Validate() error
	// Run the cmd
	Run() error
}

func main() {
	flag.Parse()

	notionClient := newNotionClient()

	if *flagMulti {
		configs := loadMultiConfig(*flagConfigPath)
		if *flagDebugMode {
			log.Printf("MultiConfig len: %v", len(configs))
		}

		repeat(func() {
			if *flagMultiIdx >= 0 {
				runCmd(notionClient, configs[*flagMultiIdx])
			} else {
				for _, cfg := range configs {
					runCmd(notionClient, cfg)
				}
			}
		}, *flagRepeat)
	} else {
		config := loadConfig(*flagConfigPath)

		repeat(func() {
			runCmd(notionClient, config)
		}, *flagRepeat)
	}
}

func repeat(do func(), times int) {
	for i := 0; i < times; i++ {
		do()
	}
}

func runCmd(notionClient *notion.Client, cfg Config) {
	if *flagDebugMode {
		log.Printf("Run cmd: %v, config: %+v", *flagCmd, cfg)
	} else {
		log.Printf("Run cmd: %v", *flagCmd)
	}

	var cmd Cmd
	switch *flagCmd {
	case "daily-journal": // create daily journal entries with title YYYY-MM-DD
		cmd = &DailyJournal{
			DebugMode:          *flagDebugMode,
			Client:             notionClient,
			DailyJournalConfig: cfg.DailyJournal,
		}
	case "weekly-journal": // create weekly journal entries with title like YYYY-MM-DD/YYYY-MM-DD
		cmd = &WeeklyJournal{
			DebugMode:           *flagDebugMode,
			Client:              notionClient,
			WeeklyJournalConfig: cfg.WeeklyJournal,
		}
	case "flashback": // get a random page from a database and resurface it
		cmd = &Flashback{
			DebugMode:       *flagDebugMode,
			Client:          notionClient,
			FlashbackConfig: cfg.Flashback,
		}
	case "duplicate": // find duplicated pages (same title) in a database
		cmd = &DuplicateChecker{
			DebugMode:              *flagDebugMode,
			Client:                 notionClient,
			DuplicateCheckerConfig: cfg.DuplicateChecker,
		}
	case "collector": // find certain pages from a database and dump the delta pages in a page
		cmd = &Collector{
			DebugMode:       *flagDebugMode,
			Client:          notionClient,
			CollectorConfig: cfg.Collector,
		}
	case "export": // export pages from a database into local folders in markdown
		cmd = &Exporter{
			DebugMode:      *flagDebugMode,
			Client:         notionClient,
			ExporterConfig: cfg.Exporter,
		}
	default:
		log.Fatalf("Unknown cmd: `%v`", *flagCmd)
	}

	if err := cmd.Validate(); err != nil {
		log.Fatalf("cmd %v validate failed: %+v", *flagCmd, err)
	}

	if err := cmd.Run(); err != nil {
		log.Fatalf("cmd %v error: %+v", *flagCmd, err)
	}

	log.Printf("cmd %v completed", *flagCmd)
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
