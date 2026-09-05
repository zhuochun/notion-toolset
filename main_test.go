package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the shipped entry point from outside its source directory.
func TestBinaryCLI(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "notion-toolset.exe")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, tc := range []struct {
		name    string
		args    []string
		token   bool
		code    int
		message string
	}{
		{"help", []string{"--help"}, false, 0, "-workspace"},
		{"unknown flag", []string{"--bogus"}, false, 2, "flag provided but not defined"},
		{"invalid flag value", []string{"--repeat=no"}, false, 2, "invalid value"},
		{"token before config", []string{"--config=missing.yaml"}, false, 1, "Empty Token"},
		{"missing config", nil, true, 2, "Error in Config File"},
		{"unknown command", []string{"--one=page", "--cmd=unknown"}, true, 1, "unknown cmd"},
		{"zero repeat", []string{"--one=page", "--cmd=unknown", "--repeat=0"}, true, 0, ""},
		{"negative repeat", []string{"--one=page", "--repeat=-1"}, true, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(binary, tc.args...)
			cmd.Dir = t.TempDir()
			for _, v := range os.Environ() {
				if !strings.HasPrefix(strings.ToUpper(v), "NOTION_TOKEN=") {
					cmd.Env = append(cmd.Env, v)
				}
			}
			if tc.token {
				cmd.Env = append(cmd.Env, "NOTION_TOKEN=local-test")
			}
			out, err := cmd.CombinedOutput()
			code := 0
			if err != nil {
				if e, ok := err.(*exec.ExitError); ok {
					code = e.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if code != tc.code || !strings.Contains(strings.ToLower(string(out)), strings.ToLower(tc.message)) {
				t.Fatalf("exit=%d, want=%d; output=%s", code, tc.code, out)
			}
		})
	}
}
