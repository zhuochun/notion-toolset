package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/transformer"
)

type DuplicateCheckerConfig struct {
	DatabaseID         string   `yaml:"databaseID"`
	DatabaseQuery      string   `yaml:"databaseQuery"`
	RecordBlockID      string   `yaml:"recordBlockID"`
	Properties         []string `yaml:"properties"`
	DuplicateDumpID    string   `yaml:"duplicateDumpID"`
	DuplicateDumpBlock string   `yaml:"duplicateDumpBlock"`
}

type DuplicateChecker struct {
	DebugMode bool

	Client *notion.Client
	DuplicateCheckerConfig
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
				d.WritePageMention(page.ID) // TODO make this one-liner
				d.WritePageMention(id)
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

func (d *DuplicateChecker) WritePageMention(pageID string) (notion.BlockChildrenResponse, error) {
	blockData, err := Tmpl("DuplicateDumpBlock", d.DuplicateDumpBlock, BlockBuilder{
		PageID: pageID,
	})
	if err != nil {
		return notion.BlockChildrenResponse{}, err
	}

	block := notion.Block{}
	if err := json.Unmarshal(blockData, &block); err != nil {
		return notion.BlockChildrenResponse{}, fmt.Errorf("unmarshal Block: %w", err)
	}

	return d.Client.AppendBlockChildren(context.TODO(), d.DuplicateDumpID, []notion.Block{block})
}
