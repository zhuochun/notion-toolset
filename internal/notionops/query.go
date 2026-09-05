package notionops

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/notionread"
	"golang.org/x/time/rate"
)

type QueryBuilder struct {
	Date  string // default date or start date
	Today string
	Title string
}

type DatabaseQuery struct {
	Client     *notion.Client
	DatabaseID string

	Query *notion.DatabaseQuery
}

func NewDatabaseQuery(c *notion.Client, databaseID string) *DatabaseQuery {
	return &DatabaseQuery{
		Client:     c,
		DatabaseID: databaseID,
		Query:      &notion.DatabaseQuery{},
	}
}

func NewReader(client *notion.Client, requestsPerSecond float64) *notionread.Reader {
	return notionread.New(client,
		notionread.WithLimiter(rate.NewLimiter(rate.Limit(requestsPerSecond), int(requestsPerSecond))),
		notionread.WithConcurrency(max(1, int(requestsPerSecond))),
	)
}

func (q *DatabaseQuery) SetQuery(queryTmpl string, builder QueryBuilder) error {
	if queryTmpl == "" {
		return nil
	}

	queryData, err := Tmpl("DatabaseQuery", queryTmpl, builder)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(queryData, q.Query); err != nil {
		return fmt.Errorf("unmarshal DatabaseQuery: %w", err)
	}

	return nil
}

func (q *DatabaseQuery) ForEach(ctx context.Context, maxResults int, reader *notionread.Reader, visit func(notion.Page) error) error {
	if reader == nil {
		reader = notionread.New(q.Client)
	}
	return reader.ForEachDatabasePage(ctx, notionread.DatabaseRead{
		DatabaseID: q.DatabaseID,
		Query:      q.Query,
		MaxResults: maxResults,
	}, visit)
}

func (q *DatabaseQuery) Once(ctx context.Context) ([]notion.Page, error) {
	return notionread.New(q.Client).QueryDatabaseOnce(ctx, q.DatabaseID, q.Query)
}

const DateLayout = "2006-01-02"
