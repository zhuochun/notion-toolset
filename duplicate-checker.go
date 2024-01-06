package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/transformer"
)

type DuplicateCheckerConfig struct {
	DatabaseID             string   `yaml:"databaseID"`
	DatabaseQuery          string   `yaml:"databaseQuery"`
	CheckProperties        []string `yaml:"checkProperties"` // TODO Check by specific properties
	DuplicateDumpID        string   `yaml:"duplicateDumpID"`
	DuplicateDumpTextBlock string   `yaml:"duplicateDumpTextBlock"` // Format https://pkg.go.dev/github.com/dstotijn/go-notion#ParagraphBlock
	// DuplicateDumpBlock string   `yaml:"duplicateDumpBlock"` // DEPRECATED (2023-12) use duplicateDumpTextBlock
}

type DuplicateChecker struct {
	DebugMode bool

	Client *notion.Client
	DuplicateCheckerConfig
}

func (d *DuplicateChecker) Validate() error {
	if len(d.DuplicateDumpTextBlock) == 0 {
		return errors.Join(ErrConfigRequired, fmt.Errorf("set duplicateDumpTextBlock"))
	}
	return nil
}

func (d *DuplicateChecker) Run() error {
	pagesChan, errChan := d.ScanPages()
	pageNum := 0
	set := map[string]string{}
	for pages := range pagesChan {
		for _, page := range pages {
			pageNum += 1

			title, err := transformer.GetPageTitle(page)
			if err != nil {
				log.Printf("Err pageID: %v, err: %v", page.ID, err)
				continue
			}

			if id, ok := set[title]; ok {
				d.WriteBlock(page.ID)
				d.WriteBlock(id)
			} else {
				set[title] = page.ID
			}

			if d.DebugMode && pageNum%500 == 0 {
				log.Printf("Scanned pages: %v so far", pageNum)
			}
		}
	}
	log.Printf("Scanned pages: %v, unique: %v", pageNum, len(set))

	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}

func (d *DuplicateChecker) ScanPages() (chan []notion.Page, chan error) {
	q := NewDatabaseQuery(d.Client, d.DatabaseID)

	if err := q.SetQuery(d.DatabaseQuery, QueryBuilder{}); err != nil {
		log.Panicf("Invalid query: %v, err: %v", d.DatabaseQuery, err)
	}

	if d.DebugMode {
		log.Printf("DatabaseQuery Filter: %+v", q.Query.Filter)
		log.Printf("DatabaseQuery Sorter: %+v", q.Query.Sorts)
	}

	return q.Go(context.TODO(), 3)
}

func (d *DuplicateChecker) WriteBlock(pageID string) (notion.BlockChildrenResponse, error) {
	w := NewAppendBlock(d.Client, d.DuplicateDumpID)

	if err := w.SetBlock("Duplicate", d.DuplicateDumpTextBlock, BlockBuilder{
		Date:   time.Now().Format(layoutDate),
		PageID: pageID,
	}); err != nil {
		return notion.BlockChildrenResponse{}, err
	}

	return w.WriteParagraph(context.TODO())
}
