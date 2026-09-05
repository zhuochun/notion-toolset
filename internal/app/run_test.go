package app

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dstotijn/go-notion"
	"github.com/sashabaranov/go-openai"
	"github.com/zhuochun/notion-toolset/internal/notiontest"
)

func TestMultiRepeatAndIndex(t *testing.T) {
	file := filepath.Join(t.TempDir(), "multi.yaml")
	if err := os.WriteFile(file, []byte("- dailyJournal:\n    databaseID: first\n- dailyJournal:\n    databaseID: second\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                string
		index, repeat, code int
		want                []string
	}{{"all", -1, 2, 0, []string{"/v1/databases/first/query", "/v1/databases/second/query", "/v1/databases/first/query", "/v1/databases/second/query"}}, {"selected", 1, 1, 0, []string{"/v1/databases/second/query"}}, {"range", 2, 1, 1, nil}, {"zero", -1, 0, 0, nil}} {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			var logs bytes.Buffer
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"object":"list","results":[],"has_more":false}`)
			}))
			defer s.Close()
			rt := Runtime{Getenv: func(k string) string {
				if k != "NOTION_TOKEN" {
					t.Errorf("unneeded environment lookup: %s", k)
				}
				return "token"
			}, Logger: log.New(&logs, "", 0), NewNotion: func(string) *notion.Client { return notiontest.Client(t, s.URL) }}
			if code := Run(Options{CommandOptions: CommandOptions{Name: "daily-journal"}, ConfigPath: file, Multi: true, Index: tc.index, Repeat: tc.repeat}, rt); code != tc.code {
				t.Fatalf("code=%d logs=%s", code, &logs)
			}
			if !reflect.DeepEqual(paths, tc.want) {
				t.Fatalf("requests=%v want=%v", paths, tc.want)
			}
		})
	}
	for _, index := range []int{-1, 0} {
		if err := os.WriteFile(file, []byte("[]"), 0600); err != nil {
			t.Fatal(err)
		}
		code := Run(Options{ConfigPath: file, Multi: true, Index: index, Repeat: 1}, Runtime{Getenv: func(string) string { return "token" }, Logger: log.New(io.Discard, "", 0)})
		want := 0
		if index == 0 {
			want = 1
		}
		if code != want {
			t.Fatalf("empty list index %d: %d", index, code)
		}
	}
}

func TestLLMEnvironmentEndpoint(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/override/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("request=%s auth=%q", r.URL, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer s.Close()
	rt := Runtime{Getenv: func(k string) string {
		return map[string]string{"DOT_OPENAI_KEY": "test-key", "DOT_OPENAI_URL": s.URL + "/override"}[k]
	}}
	c, err := rt.llmClient()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateChatCompletion(context.Background(), openai.ChatCompletionRequest{Model: "fixture", Messages: []openai.ChatCompletionMessage{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := (Runtime{Getenv: func(string) string { return "" }}).llmClient(); err == nil {
		t.Fatal("missing key accepted")
	}
}
