package flashback

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/notionops"
)

type Flashback struct {
	Now       func() time.Time
	Intn      func(int) int
	Logger    *log.Logger
	DebugMode bool

	Client *notion.Client
	config.FlashbackConfig
}

func (f *Flashback) Validate() error {
	if f.FlashbackPageID == "" && f.FlashbackJournalID == "" {
		return errors.Join(config.ErrRequired, fmt.Errorf("set flashbackPageID or flashbackJournalID"))
	}

	if f.FlashbackTextBlock == "" {
		return errors.Join(config.ErrRequired, fmt.Errorf("set flashbackTextBlock"))
	}

	return nil
}

func (f *Flashback) Run() error {
	if err := f.SetFlashbackPageID(); err != nil {
		return err
	}

	maxHours := int(f.now().Sub(f.OldestTimestamp).Hours())

	lookbackHour := f.intn(maxHours)
	pages, err := f.GetPages(time.Duration(lookbackHour) * time.Hour)
	if err != nil {
		return err
	}
	f.logger().Printf("Lookback %v Hours/%v Day, Queried pages: %+v", lookbackHour, lookbackHour/24, len(pages))

	if len(pages) < 1 {
		lookbackHour = maxHours
		pages, err = f.GetPages(time.Duration(lookbackHour) * time.Hour)
		if err != nil {
			return err
		}
		f.logger().Printf("Lookback (max) %v Hours/%v Day, Queried pages: %+v", lookbackHour, lookbackHour/24, len(pages))
	}

	if len(pages) < 1 {
		f.logger().Printf("Skipped. no pages fetched")
		return nil
	}

	if f.FlashbackNum < 1 {
		f.FlashbackNum = 1
	} else if f.FlashbackNum > len(pages) {
		f.FlashbackNum = len(pages)
	}

	picked := map[int]struct{}{}
	for i := 0; i < f.FlashbackNum; i++ {
		n := f.intn(len(pages))
		for {
			if _, found := picked[n]; found {
				n = (n + 1) % len(pages)
			} else {
				picked[n] = struct{}{}
				break
			}
		}
	}

	for n := range picked {
		if block, err := f.WriteBlock(pages[n].ID); err == nil {
			if len(block.Results) > 0 {
				f.logger().Printf("Append block child %v", block.Results[0].ID())
			}
		}
	}

	if f.FlashbackChainFile != "" {
		file, err := os.Create(f.FlashbackChainFile)
		if err != nil {
			return fmt.Errorf("create file: %v, err: %v", f.FlashbackChainFile, err)
		}
		defer file.Close()

		for n := range picked {
			if _, err = file.WriteString(pages[n].ID + "\n"); err != nil {
				f.logger().Printf("Failed writing to file, err: %v", err)
			}
		}
	}

	return nil
}

func (f *Flashback) GetPages(lookback time.Duration) ([]notion.Page, error) {
	q := notionops.NewDatabaseQuery(f.Client, f.DatabaseID)

	if err := q.SetQuery(f.DatabaseQuery, notionops.QueryBuilder{
		Date:  f.now().Add(-lookback).Format(notionops.DateLayout),
		Today: f.now().Format(notionops.DateLayout),
	}); err != nil {
		f.logger().Panicf("Invalid query: %v, err: %v", f.DatabaseQuery, err)
	}

	if f.DebugMode {
		f.logger().Printf("DatabaseQuery Filter: %+v", q.Query.Filter)
		f.logger().Printf("DatabaseQuery Sorter: %+v", q.Query.Sorts)
	}

	return q.Once(context.TODO())
}

func (f *Flashback) SetFlashbackPageID() error {
	if f.FlashbackJournalID == "" {
		return nil
	}

	now := f.now()
	title := now.Format(notionops.DateLayout)

	q := notionops.NewDatabaseQuery(f.Client, f.FlashbackJournalID)
	q.Query = &notion.DatabaseQuery{
		Filter: &notion.DatabaseQueryFilter{
			Property: "title",
			DatabaseQueryPropertyFilter: notion.DatabaseQueryPropertyFilter{
				Title: &notion.TextPropertyFilter{Equals: title},
			},
		},
		Sorts: []notion.DatabaseQuerySort{
			{Timestamp: notion.SortTimeStampCreatedTime, Direction: notion.SortDirAsc},
		},
	}

	pages, err := q.Once(context.TODO())
	if err != nil {
		return fmt.Errorf("no journal found for %v: %w", title, err)
	}
	if len(pages) == 0 {
		return fmt.Errorf("no journal found for %v", title)
	}

	if len(pages) > 1 {
		f.logger().Printf("Multiple journal found: %v, cnt: %v, uses: %v", title, len(pages), pages[0].ID)
	}

	if f.DebugMode {
		f.logger().Printf("Journal by title: %v, found: %v, uses: %v", title, len(pages), pages[0].ID)
	}

	f.FlashbackPageID = pages[0].ID
	return nil
}

func (f *Flashback) WriteBlock(pageID string) (notion.BlockChildrenResponse, error) {
	w := notionops.NewAppendBlock(f.Client, f.FlashbackPageID)

	if err := w.AddParagraph("Flashback", f.FlashbackTextBlock, notionops.BlockBuilder{
		Date:   f.now().Format(notionops.DateLayout),
		PageID: pageID,
	}); err != nil {
		return notion.BlockChildrenResponse{}, err
	}

	return w.Do(context.TODO())
}

func (f *Flashback) logger() *log.Logger {
	if f.Logger != nil {
		return f.Logger
	}
	return log.Default()
}

func (f *Flashback) intn(n int) int {
	if f.Intn != nil {
		return f.Intn(n)
	}
	return rand.Intn(n)
}

func (f *Flashback) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}
