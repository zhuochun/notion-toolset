package notionread

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/retry"
	"golang.org/x/time/rate"
)

// Reader owns retry, rate limiting, pagination, and block traversal for Notion reads.
type Reader struct {
	client      *notion.Client
	limiter     *rate.Limiter
	concurrency int
	blockGate   chan struct{}
}

type Option func(*Reader)

func WithLimiter(limiter *rate.Limiter) Option {
	return func(reader *Reader) {
		reader.limiter = limiter
	}
}

func WithConcurrency(concurrency int) Option {
	return func(reader *Reader) {
		if concurrency > 0 {
			reader.concurrency = concurrency
		}
	}
}

func New(client *notion.Client, options ...Option) *Reader {
	reader := &Reader{client: client, concurrency: 1}
	for _, option := range options {
		option(reader)
	}
	reader.blockGate = make(chan struct{}, reader.concurrency)
	return reader
}

type DatabaseRead struct {
	DatabaseID string
	Query      *notion.DatabaseQuery
	MaxResults int
}

func (r *Reader) Page(ctx context.Context, pageID string) (notion.Page, error) {
	var page notion.Page
	err := r.read(ctx, func() error {
		var err error
		page, err = r.client.FindPageByID(ctx, pageID)
		return err
	})
	if err != nil {
		return notion.Page{}, fmt.Errorf("read Notion page %s: %w", pageID, err)
	}
	return page, nil
}

// QueryDatabaseOnce performs one database query request. Use ForEachDatabasePage
// when the complete result set is required.
func (r *Reader) QueryDatabaseOnce(ctx context.Context, databaseID string, query *notion.DatabaseQuery) ([]notion.Page, error) {
	resp, err := r.databasePage(ctx, databaseID, query)
	if err != nil {
		return nil, err
	}
	return resp.Results, nil
}

func (r *Reader) ForEachDatabasePage(ctx context.Context, read DatabaseRead, visit func(notion.Page) error) error {
	query := notion.DatabaseQuery{}
	if read.Query != nil {
		query = *read.Query
	}
	query.StartCursor = ""

	delivered := 0
	for {
		if read.MaxResults > 0 {
			remaining := read.MaxResults - delivered
			if remaining <= 0 {
				return nil
			}
			pageSize := min(remaining, 100)
			if query.PageSize == 0 || query.PageSize > pageSize {
				query.PageSize = pageSize
			}
		}

		resp, err := r.databasePage(ctx, read.DatabaseID, &query)
		if err != nil {
			return err
		}
		for _, page := range resp.Results {
			if err := ctx.Err(); err != nil {
				return err
			}
			if read.MaxResults > 0 && delivered >= read.MaxResults {
				return nil
			}
			if err := visit(page); err != nil {
				return err
			}
			delivered++
		}
		if read.MaxResults > 0 && delivered >= read.MaxResults {
			return nil
		}
		if !resp.HasMore {
			return nil
		}
		if resp.NextCursor == nil || *resp.NextCursor == "" {
			return fmt.Errorf("read Notion database %s: response has more results without a cursor", read.DatabaseID)
		}
		query.StartCursor = *resp.NextCursor
	}
}

func (r *Reader) databasePage(ctx context.Context, databaseID string, query *notion.DatabaseQuery) (notion.DatabaseQueryResponse, error) {
	var resp notion.DatabaseQueryResponse
	err := r.read(ctx, func() error {
		var err error
		resp, err = r.client.QueryDatabase(ctx, databaseID, query)
		return err
	})
	if err != nil {
		return notion.DatabaseQueryResponse{}, fmt.Errorf("read Notion database %s: %w", databaseID, err)
	}
	return resp, nil
}

func (r *Reader) BlockChildren(ctx context.Context, blockID string) ([]notion.Block, error) {
	select {
	case r.blockGate <- struct{}{}:
		defer func() { <-r.blockGate }()
	case <-ctx.Done():
		return nil, fmt.Errorf("read Notion block children %s: %w", blockID, ctx.Err())
	}

	blocks := []notion.Block{}
	cursor := ""
	for {
		query := &notion.PaginationQuery{StartCursor: cursor}
		var resp notion.BlockChildrenResponse
		err := r.read(ctx, func() error {
			var err error
			resp, err = r.client.FindBlockChildrenByID(ctx, blockID, query)
			return err
		})
		if err != nil {
			return blocks, fmt.Errorf("read Notion block children %s: %w", blockID, err)
		}
		blocks = append(blocks, resp.Results...)
		if !resp.HasMore {
			return blocks, nil
		}
		if resp.NextCursor == nil || *resp.NextCursor == "" {
			return blocks, fmt.Errorf("read Notion block children %s: response has more results without a cursor", blockID)
		}
		cursor = *resp.NextCursor
	}
}

func (r *Reader) read(ctx context.Context, fn func() error) error {
	return retry.DoIfContext(ctx, func() error {
		if r.limiter != nil {
			if err := r.limiter.Wait(ctx); err != nil {
				return err
			}
		}
		return fn()
	}, isRetryable)
}

func isRetryable(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, notion.ErrRateLimited) ||
		errors.Is(err, notion.ErrConflict) ||
		errors.Is(err, notion.ErrInternalServer) ||
		errors.Is(err, notion.ErrServiceUnavailable) {
		return true
	}

	var apiErr *notion.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == 429 || apiErr.Status >= 500
	}

	var netErr net.Error
	return errors.As(err, &netErr)
}
