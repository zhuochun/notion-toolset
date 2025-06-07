package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/retry"
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

func (q *DatabaseQuery) Go(ctx context.Context, size int, rateLimiter ...*rate.Limiter) (chan []notion.Page, chan error) {
	pagesChan := make(chan []notion.Page, size)
	errChan := make(chan error, 1)

	go func() {
		cursor := ""

		for {
			if len(rateLimiter) == 1 {
				rateLimiter[0].Wait(context.Background())
			}

			q.Query.StartCursor = cursor
			var resp notion.DatabaseQueryResponse
			err := retry.Do(func() error {
				var innerErr error
				resp, innerErr = q.Client.QueryDatabase(ctx, q.DatabaseID, q.Query)
				return innerErr
			})
			if err != nil {
				errChan <- err
				break
			}

			pagesChan <- resp.Results

			if q.Query.PageSize > 0 { // hack detection to exit
				break
			}

			if resp.HasMore {
				cursor = *resp.NextCursor
			} else {
				break
			}
		}

		close(pagesChan)
	}()

	return pagesChan, errChan
}

func (q *DatabaseQuery) Once(ctx context.Context) ([]notion.Page, error) {
	var resp notion.DatabaseQueryResponse
	err := retry.Do(func() error {
		var innerErr error
		resp, innerErr = q.Client.QueryDatabase(ctx, q.DatabaseID, q.Query)
		return innerErr
	})
	if err != nil {
		return []notion.Page{}, err
	}
	return resp.Results, nil
}
