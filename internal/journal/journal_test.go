package journal

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/notiontest"
)

func TestNextMondayLocalBoundaries(t *testing.T) {
	zone := time.FixedZone("SGT", 8*3600)
	for _, tc := range []struct{ from, want string }{{"2023-12-31", "2024-01-01"}, {"2024-01-01", "2024-01-08"}, {"2024-01-31", "2024-02-05"}} {
		from, _ := time.ParseInLocation("2006-01-02", tc.from, zone)
		got := (&WeeklyJournal{}).NextMonday(from)
		if got.Format("2006-01-02") != tc.want || got.Location() != zone {
			t.Fatalf("%s -> %s", tc.from, got)
		}
	}
}

func TestJournalRunSkipsExistingAndContinuesCreateFailure(t *testing.T) {
	for _, weekly := range []bool{false, true} {
		t.Run(fmt.Sprint(weekly), func(t *testing.T) {
			existing := "2024-01-01"
			want := []string{"2024-01-02", "2024-01-03"}
			if weekly {
				existing = "2024-01-01/2024-01-07"
				want = []string{"2024-01-08/2024-01-14", "2024-01-15/2024-01-21"}
			}
			var bodies []string
			var logs bytes.Buffer
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/query") {
					b, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(b), "2023-12-31") {
						t.Errorf("query date: %s", b)
					}
					fmt.Fprintf(w, `{"object":"list","results":[{"object":"page","id":"existing","parent":{"type":"database_id","database_id":"db"},"properties":{"Name":{"id":"title","type":"title","title":[{"type":"text","plain_text":%q,"text":{"content":%q}}]}}}],"has_more":false}`, existing, existing)
					return
				}
				b, _ := io.ReadAll(r.Body)
				bodies = append(bodies, string(b))
				if len(bodies) == 1 {
					w.WriteHeader(400)
					io.WriteString(w, `{"object":"error","status":400,"code":"validation_error","message":"create rejected"}`)
					return
				}
				io.WriteString(w, `{"object":"page","id":"created","parent":{"type":"database_id","database_id":"db"},"properties":{}}`)
			}))
			defer server.Close()
			now := func() time.Time { return time.Date(2023, 12, 31, 23, 0, 0, 0, time.FixedZone("SGT", 28800)) }
			query := `{"filter":{"property":"Name","title":{"equals":"{{.Date}}"}}}`
			props := `{"Name":{"title":[{"text":{"content":"{{.Title}}"}}]},"Date":{"date":{"start":"{{.Date}}"}}}`
			var err error
			if weekly {
				d := WeeklyJournal{Client: notiontest.Client(t, server.URL), Now: now, Logger: log.New(&logs, "", 0), WeeklyJournalConfig: config.WeeklyJournalConfig{DatabaseID: "db", Limit: 3, PageQuery: query, PageProperties: props}}
				err = d.Run()
			} else {
				d := DailyJournal{Client: notiontest.Client(t, server.URL), Now: now, Logger: log.New(&logs, "", 0), DailyJournalConfig: config.DailyJournalConfig{DatabaseID: "db", Limit: 3, PageQuery: query, PageProperties: props}}
				err = d.Run()
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(bodies) != 2 {
				t.Fatalf("creates: %v", bodies)
			}
			for i, title := range want {
				if !strings.Contains(bodies[i], title) {
					t.Fatalf("create %d: %s", i, bodies[i])
				}
			}
			if !strings.Contains(logs.String(), "create rejected") || !strings.Contains(logs.String(), "Created page") {
				t.Fatalf("continuation log: %s", &logs)
			}
		})
	}
}
