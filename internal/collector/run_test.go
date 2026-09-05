package collector

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/notiontest"
)

func TestRunCompletesDiscoveryAndScanBeforeWriting(t *testing.T) {
	for _, mode := range []string{"success", "discovery-error", "later-scan-error", "nothing-new", "write-error"} {
		t.Run(mode, func(t *testing.T) {
			queries, writes := 0, 0
			var body string
			var logs bytes.Buffer
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fail := func() {
					w.WriteHeader(400)
					io.WriteString(w, `{"object":"error","status":400,"code":"validation_error","message":"fixture failure"}`)
				}
				switch {
				case r.Method == "GET":
					if mode == "discovery-error" {
						fail()
						return
					}
					io.WriteString(w, `{"object":"list","results":[{"object":"block","id":"mention","type":"paragraph","paragraph":{"rich_text":[{"type":"mention","mention":{"type":"page","page":{"id":"A"}}}]}}],"has_more":false}`)
				case strings.Contains(r.URL.Path, "/query"):
					queries++
					if queries == 2 {
						fail()
						return
					}
					results := `{"object":"page","id":"A","parent":{"type":"database_id","database_id":"db"},"properties":{}}`
					if mode != "nothing-new" {
						results += `,{"object":"page","id":"B","parent":{"type":"database_id","database_id":"db"},"properties":{}}`
					}
					more := `"has_more":false`
					if mode == "later-scan-error" {
						more = `"has_more":true,"next_cursor":"next"`
					}
					io.WriteString(w, `{"object":"list","results":[`+results+`],`+more+`}`)
				case r.Method == "PATCH":
					writes++
					b, _ := io.ReadAll(r.Body)
					body = string(b)
					if mode == "write-error" {
						fail()
						return
					}
					io.WriteString(w, `{"object":"list","results":[]}`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			c := Collector{Client: notiontest.Client(t, server.URL), Logger: log.New(&logs, "", 0), CollectorConfig: config.CollectorConfig{DatabaseID: "db", CollectionIDs: []string{"collection"}, CollectDumpID: "dump", CollectDumpTextBlock: `{"rich_text":[{"type":"text","text":{"content":"{{.PageID}}"}}]}`}}
			err := c.Run()
			wantErr := mode == "discovery-error" || mode == "later-scan-error"
			if (err != nil) != wantErr {
				t.Fatalf("Run=%v", err)
			}
			wantWrites := 0
			if mode == "success" || mode == "write-error" {
				wantWrites = 1
			}
			if writes != wantWrites {
				t.Fatalf("writes=%d, want %d", writes, wantWrites)
			}
			if writes > 0 && (!strings.Contains(body, `"content":"B"`) || strings.Contains(body, `"content":"A"`)) {
				t.Fatalf("wrong collected IDs: %s", body)
			}
			if mode == "write-error" && !strings.Contains(logs.String(), "failed: 1") {
				t.Fatalf("missing partial failure count: %s", &logs)
			}
		})
	}
}
