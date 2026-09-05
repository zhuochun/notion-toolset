package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestInvocationsDoNotLeakFlags(t *testing.T) {
	var out bytes.Buffer
	env := func(string) string { return "local-token" }
	if code := Run("tool", []string{"--one=page", "--repeat=0", "--cmd=unknown"}, env, &out); code != 0 {
		t.Fatalf("first: %d %s", code, &out)
	}
	out.Reset()
	if code := Run("tool", nil, env, &out); code != 2 || !strings.Contains(out.String(), "Error in Config File") {
		t.Fatalf("second inherited flags: %d %s", code, &out)
	}
	out.Reset()
	if code := Run("tool", []string{"--help"}, env, &out); code != 0 || !strings.Contains(out.String(), "(default 1)") {
		t.Fatalf("help: %d %s", code, &out)
	}
}
