package app

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/zhuochun/notion-toolset/internal/config"
)

type Options struct {
	CommandOptions
	ConfigPath string
	Multi      bool
	Index      int
	Repeat     int
}

var hashIDRegex = regexp.MustCompile("([a-zA-Z0-9]{32})")

// Run returns the existing process exit classification; it never exits itself.
func Run(opts Options, rt Runtime) int {
	logger := rt.logger()
	if opts.ExecOne != "" && strings.HasPrefix(opts.ExecOne, "https:") {
		if matches := hashIDRegex.FindStringSubmatch(opts.ExecOne); len(matches) > 1 {
			opts.ExecOne = matches[1]
			logger.Printf("Parsed ID: %v", opts.ExecOne)
		}
	}
	token := rt.getenv("NOTION_TOKEN")
	if token == "" {
		logger.Println("Empty Token in env.NOTION_TOKEN")
		return 1
	}
	client := rt.notionClient(token)
	var configs []config.Config
	if opts.Multi {
		var err error
		configs, err = config.LoadMulti(opts.ConfigPath)
		if err != nil {
			logger.Print(err)
			return 2
		}
		if opts.DebugMode {
			logger.Printf("MultiConfig len: %v", len(configs))
		}
		if opts.Index >= len(configs) {
			logger.Print(fmt.Sprintf("config index out of range: idx=%d, len=%d", opts.Index, len(configs)))
			return 1
		}
		if opts.Index >= 0 {
			configs = configs[opts.Index : opts.Index+1]
		}
	} else {
		cfg, err := config.Load(opts.ConfigPath, opts.ExecOne)
		if err != nil {
			logger.Print(err)
			return 2
		}
		configs = []config.Config{cfg}
	}
	for i := 0; i < opts.Repeat; i++ {
		for _, cfg := range configs {
			if err := runCmd(rt, client, cfg, opts.CommandOptions); err != nil {
				logger.Print(err)
				return 1
			}
		}
	}
	return 0
}
