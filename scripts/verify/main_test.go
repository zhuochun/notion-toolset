package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Real temporary modules exercise failure propagation without altering the repo
// or recursively invoking the verifier from its own test suite.
func fixtureModule(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":        "module verify-fixture\n\ngo 1.25.0\n",
		"probe_test.go": "package probe\nimport \"testing\"\nfunc TestProbe(t *testing.T) { " + body + " }\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOWORK", "off")
}

func TestVerifyRejectsFocusedArguments(t *testing.T) {
	var output bytes.Buffer
	err := verify([]string{"-run=TestProbe"}, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "no arguments accepted") {
		t.Fatalf("expected argument rejection, got %v", err)
	}
}

func TestVerifyRejectsGOFLAGSSelector(t *testing.T) {
	fixtureModule(t, `t.Fatal("must not be silently skipped")`)
	t.Setenv("GOFLAGS", "-run=^$")
	var output bytes.Buffer
	err := verify(nil, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "empty GOFLAGS") {
		t.Fatalf("expected GOFLAGS rejection, got %v; output: %s", err, &output)
	}
}

func TestVerifyReportsWrongDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	err := verify(nil, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "repository root") {
		t.Fatalf("expected root diagnostic, got %v", err)
	}
}

func TestVerifyStopsOnRealTestFailure(t *testing.T) {
	fixtureModule(t, `t.Fatal("verification negative control")`)
	var output bytes.Buffer
	err := verify(nil, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "go test -count=1 ./... failed") {
		t.Fatalf("expected failing test rejection, got %v; output: %s", err, &output)
	}
	if !strings.Contains(output.String(), "verification negative control") || strings.Contains(output.String(), "+ go vet") {
		t.Fatalf("expected original diagnostic and stop before vet: %s", &output)
	}
}

func TestVerifyRunsAllStagesOnSuccess(t *testing.T) {
	fixtureModule(t, "")
	var output bytes.Buffer
	if err := verify(nil, &output, &output); err != nil {
		t.Fatalf("verify: %v; output: %s", err, &output)
	}
	for _, label := range []string{"+ go test -count=1 ./...", "+ go vet ./...", "+ go build ./..."} {
		if !strings.Contains(output.String(), label) {
			t.Errorf("missing %q in %s", label, &output)
		}
	}
}
