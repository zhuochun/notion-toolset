package upload

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/notiontest"
	"github.com/zhuochun/notion-toolset/notionread"
	"github.com/zhuochun/notion-toolset/transformer"
)

func TestUploadModeEffects(t *testing.T) {
	for _, mode := range []string{"dry-run", "discard", "upload", "conflict", "match", "no-candidates"} {
		t.Run(mode, func(t *testing.T) {
			root := gitFixture(t)
			id := strings.Repeat("a", 32)
			path := filepath.Join(root, id+".md")
			writeFixture(t, path, "base\n\n")
			runGit(t, root, "add", ".")
			runGit(t, root, "commit", "-m", "fixture")
			local, remote := "local\n\n", "base"
			if mode == "discard" || mode == "match" {
				remote = "local"
			}
			if mode == "conflict" {
				remote = "remote"
			}
			if mode != "no-candidates" {
				writeFixture(t, path, local)
			}
			writes, factories, reads := 0, 0, 0
			var logs bytes.Buffer
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method != "GET" {
					writes++
					if r.Method == "DELETE" {
						io.WriteString(w, `{"object":"block","id":"p","type":"paragraph","paragraph":{"rich_text":[]}}`)
					} else {
						io.WriteString(w, `{"object":"list","results":[]}`)
					}
					return
				}
				reads++
				if strings.Contains(r.URL.Path, "/pages/") {
					fmt.Fprintf(w, `{"object":"page","id":%q,"parent":{"type":"database_id","database_id":"db"},"properties":{}}`, id)
				} else {
					fmt.Fprintf(w, `{"object":"list","results":[{"object":"block","id":"p","type":"paragraph","paragraph":{"rich_text":[{"type":"text","annotations":{},"plain_text":%q,"text":{"content":%q}}]}}],"has_more":false}`, remote, remote)
				}
			}))
			defer s.Close()
			client := notiontest.Client(t, s.URL)
			r := ReverseUploader{repoRoot: root, Mode: mode, Client: client, Logger: log.New(&logs, "", 0), NewReader: func(float64) *notionread.Reader { factories++; return notionread.New(client) }, ExporterConfig: config.ExporterConfig{Directory: root, ExportSpeed: 3, Markdown: transformer.MarkdownConfig{NoAlias: true, NoMetadata: true, NoFrontMatters: true}}}
			if mode == "conflict" || mode == "match" {
				r.Mode = "upload"
			}
			err := r.Run()
			if (err != nil) != (mode == "conflict") {
				t.Fatalf("Run=%v logs=%s", err, &logs)
			}
			wantWrites := 0
			if mode == "upload" {
				wantWrites = 2
			}
			if writes != wantWrites {
				t.Fatalf("writes=%d want=%d logs=%s", writes, wantWrites, &logs)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := local
			if mode == "discard" || mode == "no-candidates" {
				want = "base\n\n"
			}
			if string(b) != want {
				t.Fatalf("local file changed: %q", b)
			}
			if mode == "no-candidates" {
				if factories != 0 || reads != 0 {
					t.Fatalf("eager resource setup: factory=%d reads=%d", factories, reads)
				}
			} else if factories != 1 {
				t.Fatalf("factory=%d", factories)
			}
		})
	}
}
