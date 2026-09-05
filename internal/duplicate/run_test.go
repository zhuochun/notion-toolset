package duplicate

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/notiontest"
)

func TestDuplicateORPropertiesReportEachPageOnce(t *testing.T) {
	var reports []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "PATCH" {
			b, _ := io.ReadAll(r.Body)
			reports = append(reports, string(b))
			io.WriteString(w, `{"object":"list","results":[]}`)
			return
		}
		var pages []string
		for _, p := range []struct{ id, a, b string }{{"A", "same", "first"}, {"B", "same", "second"}, {"C", "third", "second"}, {"D", "", ""}} {
			pages = append(pages, fmt.Sprintf(`{"object":"page","id":%q,"parent":{"type":"database_id","database_id":"db"},"properties":{"URL":{"type":"url","url":%q},"Other":{"type":"url","url":%q}}}`, p.id, p.a, p.b))
		}
		io.WriteString(w, `{"object":"list","results":[`+strings.Join(pages, ",")+`],"has_more":false}`)
	}))
	defer s.Close()
	d := DuplicateChecker{Client: notiontest.Client(t, s.URL), DuplicateCheckerConfig: config.DuplicateCheckerConfig{DatabaseID: "db", CheckProperties: []string{"URL", "Other"}, DuplicateDumpID: "target", DuplicateDumpTextBlock: `{"rich_text":[{"text":{"content":"{{.PageID}}"}}]}`}}
	if err := d.Run(); err != nil {
		t.Fatal(err)
	}
	if len(reports) != 3 {
		t.Fatalf("reports=%v", reports)
	}
	for i, id := range []string{"A", "B", "C"} {
		if !strings.Contains(reports[i], `"content":"`+id+`"`) {
			t.Fatalf("report %d=%s", i, reports[i])
		}
	}
}
