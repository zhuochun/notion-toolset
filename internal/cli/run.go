// Package cli owns flag parsing and the process-facing invocation boundary.
package cli

import (
	"flag"
	"io"
	"log"

	"github.com/zhuochun/notion-toolset/internal/app"
)

// Run uses a fresh flag set and logger so consecutive calls cannot leak options.
// The flag package's help and diagnostic output remains on stderr.
func Run(name string, args []string, getenv func(string) string, stderr io.Writer) int {
	opts := app.Options{}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&opts.Name, "cmd", "", "Run command")
	fs.StringVar(&opts.ExecOne, "one", "", "Run with one ID")
	fs.BoolVar(&opts.Multi, "multi", false, "Multiple configs in the config file")
	fs.IntVar(&opts.Index, "idx", -1, "Use a specific config by index in the multiple config")
	fs.IntVar(&opts.Repeat, "repeat", 1, "Repeat this command")
	fs.StringVar(&opts.ConfigPath, "config", "", "Path to config file")
	fs.BoolVar(&opts.DebugMode, "debug", false, "Enable debug mode")
	fs.StringVar(&opts.Mode, "mode", "dry-run", "Command mode")
	fs.StringVar(&opts.Workspace, "workspace", "https://www.notion.so", "Notion workspace host or base URL for upload resolve mode")
	fs.IntVar(&opts.ResolvePort, "port", 17889, "Local port for upload resolve mode UI")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	return app.Run(opts, app.Runtime{Getenv: getenv, Logger: log.New(stderr, "", log.LstdFlags)})
}
