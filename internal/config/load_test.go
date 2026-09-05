package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeDecodeContracts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	for _, input := range []string{"unknownSection: true\nexporter:\n  unknownField: ignored\n  exportSpeed: 0\nllm:\n  temperature: null\n", "null\n", ""} {
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path, "")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Exporter.ExportSpeed != 0 || cfg.LLM.Temperature != nil {
			t.Fatalf("decoder applied workflow defaults: %+v", cfg)
		}
	}
	if _, err := Load("", "page"); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("", ""); err == nil {
		t.Fatal("missing file accepted")
	}
	for _, input := range []string{"[]", "null", "- dailyJournal:\n    limit: 0\n- llm:\n    temperature: 0\n"} {
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		cfgs, err := LoadMulti(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(cfgs) > 0 && (len(cfgs) != 2 || cfgs[1].LLM.Temperature == nil || *cfgs[1].LLM.Temperature != 0) {
			t.Fatalf("explicit zero lost: %+v", cfgs)
		}
	}
	if err := os.WriteFile(path, []byte("exporter: ["), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, ""); err == nil {
		t.Fatal("malformed YAML accepted")
	}
}
