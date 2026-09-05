package notionops

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/internal/notiontest"
)

func TestTemplatesPreserveHTMLEscaping(t *testing.T) {
	got, err := Tmpl("fixture", `{"title":"{{.Title}}","date":"{{.Date}}"}`, PageBuilder{Title: `A "quote" & <tag>`, Date: "2026-09-05"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "&#34;quote&#34; &amp; &lt;tag&gt;") {
		t.Fatalf("escaping=%s", got)
	}
	if _, err := Tmpl("bad", `{{`, nil); err == nil {
		t.Fatal("invalid template accepted")
	}
}

func TestAppendBatchesAndStopsAfterFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		var sizes []int
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var payload struct{ Children []json.RawMessage }
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			sizes = append(sizes, len(payload.Children))
			w.Header().Set("Content-Type", "application/json")
			if fail && len(sizes) == 2 {
				w.WriteHeader(400)
				io.WriteString(w, `{"object":"error","status":400,"code":"validation_error","message":"fixture failure"}`)
				return
			}
			io.WriteString(w, `{"object":"list","results":[]}`)
		}))
		a := NewAppendBlock(notiontest.Client(t, s.URL), "target")
		for i := 0; i < 201; i++ {
			a.Blocks = append(a.Blocks, &notion.ParagraphBlock{})
		}
		_, err := a.Do(context.Background())
		s.Close()
		if (err != nil) != fail {
			t.Fatalf("Do=%v", err)
		}
		want := []int{100, 100, 1}
		if fail {
			want = want[:2]
		}
		if !reflect.DeepEqual(sizes, want) {
			t.Fatalf("sizes=%v", sizes)
		}
	}
}
