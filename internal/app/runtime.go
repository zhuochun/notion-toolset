package app

import (
	"fmt"
	"log"
	"os"

	"github.com/dstotijn/go-notion"
	"github.com/sashabaranov/go-openai"
	"github.com/zhuochun/notion-toolset/internal/notionops"
	"github.com/zhuochun/notion-toolset/notionread"
)

// Runtime supplies process inputs and external clients without global mutation.
// Zero fields use the production defaults.
type Runtime struct {
	Getenv    func(string) string
	Logger    *log.Logger
	NewNotion func(string) *notion.Client
}

func (r Runtime) getenv(key string) string {
	if r.Getenv != nil {
		return r.Getenv(key)
	}
	return os.Getenv(key)
}

func (r Runtime) logger() *log.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return log.Default()
}

func (r Runtime) notionClient(token string) *notion.Client {
	if r.NewNotion != nil {
		return r.NewNotion(token)
	}
	return notion.NewClient(token)
}

func (r Runtime) llmClient() (*openai.Client, error) {
	token := r.getenv("DOT_OPENAI_KEY")
	if token == "" {
		return nil, fmt.Errorf("missing token in env.DOT_OPENAI_KEY")
	}
	cfg := openai.DefaultConfig(token)
	if url := r.getenv("DOT_OPENAI_URL"); url != "" {
		cfg.BaseURL = url
	}
	return openai.NewClientWithConfig(cfg), nil
}

func readerFactory(client *notion.Client) func(float64) *notionread.Reader {
	return func(speed float64) *notionread.Reader { return notionops.NewReader(client, speed) }
}
