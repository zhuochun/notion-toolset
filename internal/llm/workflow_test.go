package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/sashabaranov/go-openai"
	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/notiontest"
)

func TestLLMRequestAndWriteOutcomes(t *testing.T) {
	for _, mode := range []string{"text", "json", "completion-error", "write-error"} {
		t.Run(mode, func(t *testing.T) {
			writes := 0
			var request openai.ChatCompletionRequest
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "chat/completions") {
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					if mode == "completion-error" {
						w.WriteHeader(400)
						io.WriteString(w, `{"error":{"message":"completion failed","type":"invalid_request_error"}}`)
						return
					}
					content := "- result\n\nsecond"
					if mode == "json" {
						content = `{"answer":"JSON result"}`
					}
					fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q}}]}`, content)
				} else {
					writes++
					b, _ := io.ReadAll(r.Body)
					want := "result"
					if mode == "json" {
						want = "JSON result"
					}
					if !strings.Contains(string(b), want) {
						t.Errorf("write=%s", b)
					}
					if mode == "write-error" {
						w.WriteHeader(400)
						io.WriteString(w, `{"object":"error","status":400,"code":"validation_error","message":"write failed"}`)
						return
					}
					io.WriteString(w, `{"object":"list","results":[]}`)
				}
			}))
			defer s.Close()
			oc := openai.DefaultConfig("test")
			oc.BaseURL = s.URL
			temperature := float32(0.4)
			m := LangModel{Client: notiontest.Client(t, s.URL), OpenaiClient: openai.NewClientWithConfig(oc), LangModelConfig: config.LangModelConfig{Prompt: "system prompt", Model: "fixture-model", Temperature: &temperature, RespJSON: mode == "json"}}
			if mode == "json" {
				m.RespTextBlock = `[{"rich_text":[{"text":{"content":"{{.answer}}"}}]}]`
			}
			err := m.runLLMContent(notion.Page{ID: "target"}, "source content")
			if (err != nil) != (mode == "completion-error" || mode == "write-error") {
				t.Fatalf("result=%v", err)
			}
			wantWrites := 1
			if mode == "completion-error" {
				wantWrites = 0
			}
			if writes != wantWrites {
				t.Fatalf("writes=%d", writes)
			}
			if request.Model != "fixture-model" || request.Temperature != temperature || len(request.Messages) != 2 || request.Messages[0].Role != "system" || request.Messages[0].Content != "system prompt" || request.Messages[1].Role != "user" || request.Messages[1].Content != "source content" {
				t.Fatalf("request=%+v", request)
			}
			if (request.ResponseFormat != nil) != (mode == "json") {
				t.Fatalf("JSON format=%+v", request.ResponseFormat)
			}
		})
	}
}

func TestLLMChainGroupFilteringAndFailures(t *testing.T) {
	for _, mode := range []string{"per-page", "group", "min", "max", "group-missing-journal", "per-page-completion-error", "group-completion-error"} {
		t.Run(mode, func(t *testing.T) {
			completions, writes := 0, 0
			var logs bytes.Buffer
			var mu sync.Mutex
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "chat/completions"):
					completions++
					var req openai.ChatCompletionRequest
					json.NewDecoder(r.Body).Decode(&req)
					if !strings.Contains(req.Messages[1].Content, "source") {
						t.Errorf("missing page content: %+v", req)
					}
					if strings.Contains(mode, "completion-error") {
						w.WriteHeader(400)
						io.WriteString(w, `{"error":{"message":"completion failed"}}`)
						return
					}
					io.WriteString(w, `{"choices":[{"message":{"content":"result"}}]}`)
				case r.Method == "PATCH":
					writes++
					io.WriteString(w, `{"object":"list","results":[]}`)
				case strings.HasSuffix(r.URL.Path, "/query"):
					io.WriteString(w, `{"object":"list","results":[],"has_more":false}`)
				case strings.Contains(r.URL.Path, "/pages/"):
					fmt.Fprintf(w, `{"object":"page","id":%q,"parent":{"type":"database_id","database_id":"db"},"properties":{"Name":{"id":"title","type":"title","title":[]}}}`, filepath.Base(r.URL.Path))
				default:
					io.WriteString(w, `{"object":"list","results":[{"object":"block","id":"p","type":"paragraph","paragraph":{"rich_text":[{"type":"text","annotations":{},"plain_text":"source","text":{"content":"source"}}]}}],"has_more":false}`)
				}
			}))
			defer s.Close()
			chain := filepath.Join(t.TempDir(), "chain.txt")
			if err := os.WriteFile(chain, []byte("page-1\r\n\r\npage-2\n"), 0600); err != nil {
				t.Fatal(err)
			}
			oc := openai.DefaultConfig("test")
			oc.BaseURL = s.URL
			m := LangModel{Client: notiontest.Client(t, s.URL), OpenaiClient: openai.NewClientWithConfig(oc), Logger: log.New(&logs, "", 0), Now: func() time.Time { return time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC) }, LangModelConfig: config.LangModelConfig{Prompt: "prompt", TaskSpeed: 3, ChainFile: chain, GroupExec: strings.HasPrefix(mode, "group")}}
			if mode == "min" {
				m.PageMinChars = 100
			}
			if mode == "max" {
				m.PageMaxChars = 1
			}
			if mode == "group-missing-journal" {
				m.GroupJournalID = "journal"
			}
			if err := m.Validate(); err != nil {
				t.Fatal(err)
			}
			err := m.Run()
			wantErr := mode == "group-missing-journal" || mode == "group-completion-error"
			if (err != nil) != wantErr {
				t.Fatalf("Run=%v logs=%s", err, &logs)
			}
			want := 2
			if m.GroupExec {
				want = 1
			}
			if mode == "min" || mode == "max" || mode == "group-missing-journal" {
				want = 0
			}
			if completions != want {
				t.Fatalf("completions=%d want=%d", completions, want)
			}
			wantWrites := want
			if strings.Contains(mode, "completion-error") {
				wantWrites = 0
			}
			if writes != wantWrites {
				t.Fatalf("writes=%d want=%d", writes, wantWrites)
			}
			if mode == "per-page-completion-error" && !strings.Contains(logs.String(), "Failed to run LLM") {
				t.Fatal("worker failure not logged")
			}
		})
	}
}
