// Command verify runs the repository's fixed test, vet, and build checks.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func main() {
	if err := verify(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "verify:", err)
		os.Exit(1)
	}
}

func verify(args []string, stdout, stderr io.Writer) error {
	if len(args) != 0 {
		return fmt.Errorf("no arguments accepted; use direct go test commands for focused checks")
	}
	if _, err := os.Stat("go.mod"); err != nil {
		return fmt.Errorf("run go run ./scripts/verify from the repository root: %w", err)
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("Go must be installed and available on PATH: %w", err)
	}
	flags, err := exec.Command(goPath, "env", "GOFLAGS").CombinedOutput()
	if err != nil {
		return fmt.Errorf("read Go configuration: %w: %s", err, flags)
	}
	if strings.TrimSpace(string(flags)) != "" {
		return fmt.Errorf("full verification requires empty GOFLAGS; clear shell and go env -w GOFLAGS settings, then retry")
	}
	for _, args := range [][]string{
		{"test", "-count=1", "./..."},
		{"vet", "./..."},
		{"build", "./..."},
	} {
		label := "go " + strings.Join(args, " ")
		fmt.Fprintln(stdout, "+", label)
		cmd := exec.Command(goPath, args...)
		cmd.Stdout, cmd.Stderr = stdout, stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s failed: %w", label, err)
		}
	}
	return nil
}
