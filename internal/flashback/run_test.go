package flashback

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/notiontest"
)

func TestFlashbackFallbackAndChain(t *testing.T) {
	queries, writes := 0, 0
	chain := filepath.Join(t.TempDir(), "chain.txt")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "PATCH" {
			writes++
			b, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(b), "chosen") {
				t.Errorf("write=%s", b)
			}
			io.WriteString(w, `{"object":"list","results":[]}`)
			return
		}
		queries++
		if queries == 1 {
			io.WriteString(w, `{"object":"list","results":[],"has_more":false}`)
		} else {
			fmt.Fprint(w, `{"object":"list","results":[{"object":"page","id":"chosen","parent":{"type":"database_id","database_id":"db"},"properties":{}}],"has_more":false}`)
		}
	}))
	defer s.Close()
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	f := Flashback{Client: notiontest.Client(t, s.URL), Logger: log.New(io.Discard, "", 0), Now: func() time.Time { return now }, Intn: func(n int) int {
		if n != 48 && n != 1 {
			t.Errorf("random bound=%d", n)
		}
		return 0
	}, FlashbackConfig: config.FlashbackConfig{DatabaseID: "db", OldestTimestamp: now.Add(-48 * time.Hour), FlashbackPageID: "target", FlashbackTextBlock: `{"rich_text":[{"text":{"content":"{{.PageID}}"}}]}`, FlashbackChainFile: chain}}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := f.Run(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(chain)
	if err != nil || string(b) != "chosen\n" || queries != 2 || writes != 1 {
		t.Fatalf("chain=%q queries=%d writes=%d err=%v", b, queries, writes, err)
	}
}
