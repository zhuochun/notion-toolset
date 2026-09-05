package main

import (
	"os"

	"github.com/zhuochun/notion-toolset/internal/cli"
)

func main() { os.Exit(cli.Run(os.Args[0], os.Args[1:], os.Getenv, os.Stderr)) }
