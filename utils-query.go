package main

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

func (q *DatabaseQuery) ForEach(ctx context.Context, maxResults int, limiter *rate.Limiter, visit func(notion.Page) error) error {
	return notionread.New(q.Client, notionread.WithLimiter(limiter)).ForEachDatabasePage(ctx, notionread.DatabaseRead{
		DatabaseID: q.DatabaseID,
		Query:      q.Query,
		MaxResults: maxResults,
	}, visit)
}

func (q *DatabaseQuery) Once(ctx context.Context) ([]notion.Page, error) {
	return notionread.New(q.Client).QueryDatabaseOnce(ctx, q.DatabaseID, q.Query)
}
