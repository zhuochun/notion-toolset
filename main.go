package main

import (
	"flag"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/go-yaml/yaml"
)

const (
	layoutDate = "2006-01-02"
)

var (
	flagCmd        = flag.String("cmd", "", "Run command")
	flagMulti      = flag.Bool("multi", false, "Multiple configs in the config file")
	flagMultiIdx   = flag.Int("idx", -1, "Use one specific index in the multiple config") // start from index 0
	flagConfigPath = flag.String("config", "", "Path to config file")
	flagDebugMode  = flag.Bool("debug", false, "Enable debug mode")
)

type Config struct {
	Flashback        FlashbackConfig        `yaml:"flashback"`
	DailyJournal     DailyJournalConfig     `yaml:"dailyJournal"`
	WeeklyJournal    WeeklyJournalConfig    `yaml:"weeklyJournal"`
	DuplicateChecker DuplicateCheckerConfig `yaml:"duplicateChecker"`
	Collector        CollectorConfig        `yaml:"collector"`
	Cluster          ClusterConfig          `yaml:"cluster"`
	Exporter         ExporterConfig         `yaml:"exporter"`
}

type Cmd interface {
	Run() error
}

func main() {
	flag.Parse()
	rand.Seed(time.Now().Unix())

	notionClient := newNotionClient()

	if *flagMulti {
		configs := loadMultiConfig(*flagConfigPath)
		if *flagDebugMode {
			log.Printf("MultiConfig len: %v", len(configs))
		}

		if *flagMultiIdx >= 0 {
			runCmd(notionClient, configs[*flagMultiIdx])
		} else {
			for _, cfg := range configs {
				runCmd(notionClient, cfg)
			}
		}
	} else {
		config := loadConfig(*flagConfigPath)
		runCmd(notionClient, config)
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
	case "flashback":
		cmd = &Flashback{
			DebugMode:       *flagDebugMode,
			Client:          notionClient,
			FlashbackConfig: cfg.Flashback,
		}
	case "daily-journal":
		cmd = &DailyJournal{
			DebugMode:          *flagDebugMode,
			Client:             notionClient,
			DailyJournalConfig: cfg.DailyJournal,
		}
	case "weekly-journal":
		cmd = &WeeklyJournal{
			DebugMode:           *flagDebugMode,
			Client:              notionClient,
			WeeklyJournalConfig: cfg.WeeklyJournal,
		}
	case "duplicate":
		cmd = &DuplicateChecker{
			DebugMode:              *flagDebugMode,
			Client:                 notionClient,
			DuplicateCheckerConfig: cfg.DuplicateChecker,
		}
	case "collector":
		cmd = &Collector{
			DebugMode:       *flagDebugMode,
			Client:          notionClient,
			CollectorConfig: cfg.Collector,
		}
	case "cluster":
		cmd = &Cluster{
			DebugMode:     *flagDebugMode,
			Client:        notionClient,
			ClusterConfig: cfg.Cluster,
		}
	case "export":
		cmd = &Exporter{
			DebugMode:      *flagDebugMode,
			Client:         notionClient,
			ExporterConfig: cfg.Exporter,
		}
	default:
		log.Fatalf("Unknown cmd: `%v`", *flagCmd)
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
	configFile, err := ioutil.ReadFile(configPath)
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
	configFile, err := ioutil.ReadFile(configPath)
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
