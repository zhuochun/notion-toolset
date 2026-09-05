package flashback

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/notiontest"
)

func TestSetFlashbackPageIDReturnsErrorWhenJournalMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","results":[],"next_cursor":null,"has_more":false,"type":"page_or_database","page_or_database":{}}`))
	}))
	defer server.Close()

	client := notiontest.Client(t, server.URL)
	f := &Flashback{
		Client: client,
		FlashbackConfig: config.FlashbackConfig{
			FlashbackJournalID: "journal-db",
		},
	}

	if err := f.SetFlashbackPageID(); err == nil {
		t.Fatalf("expected missing journal error")
	}
}
