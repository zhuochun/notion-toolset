package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/dstotijn/go-notion"
)

type FlashbackConfig struct {
	DatabaseID         string    `yaml:"databaseID"`
	DatabaseQuery      string    `yaml:"databaseQuery"`
	OldestTimestamp    time.Time `yaml:"oldestTimestamp"` // in format time.RFC3339 2006-01-02T15:04:05Z07:00
	FlashbackNum       int       `yaml:"flashbackNum"`    // Number of flashback entries
	FlashbackPageID    string    `yaml:"flashbackPageID"` // Page to write the flashback
	FlashbackPageBlock string    `yaml:"flashbackPageBlock"`
	UserID             string    `yaml:"userID"`
}

type Flashback struct {
	DebugMode bool

	Client *notion.Client
	FlashbackConfig
}

func (f *Flashback) Run() error {
	maxHours := int(time.Since(f.OldestTimestamp).Hours())
	lookbackHour := rand.Intn(maxHours)

	pages, err := f.GetPages(time.Duration(lookbackHour) * time.Hour)
	if err != nil {
		return err
	}

	log.Printf("Lookback %v Hours/%v Day, Queried pages: %+v", lookbackHour, lookbackHour/24, len(pages))
	if len(pages) <= 0 {
		return fmt.Errorf("skipped. no pages fetched")
	}

	n := rand.Intn(len(pages))
	if block, err := f.WriteFlashback(pages[n]); err == nil {
		if len(block.Results) > 0 {
			log.Printf("Append block child %v", block.Results[0].ID)
		}
		return err
	} else {
		return err
	}
}

func (f *Flashback) GetPages(lookback time.Duration) ([]notion.Page, error) {
	queryData, err := Tmpl("DatabaseQuery", f.DatabaseQuery, QueryBuilder{
		Date: time.Now().Add(-lookback).Format(layoutDate),
	})
	if err != nil {
		return []notion.Page{}, err
	}

	query := &notion.DatabaseQuery{}
	if err := json.Unmarshal(queryData, query); err != nil {
		return []notion.Page{}, fmt.Errorf("unmarshal DatabaseQuery: %w", err)
	}

	if f.DebugMode {
		log.Printf("DatabaseQuery Filter: %+v", query.Filter)
		log.Printf("DatabaseQuery Sorter: %+v", query.Sorts)
	}

	resp, err := f.Client.QueryDatabase(context.TODO(), f.DatabaseID, query)
	if err != nil {
		return []notion.Page{}, err
	}

	return resp.Results, nil
}

func (f *Flashback) WriteFlashback(page notion.Page) (notion.BlockChildrenResponse, error) {
	blockData, err := Tmpl("FlashbackBlock", f.FlashbackPageBlock, BlockBuilder{
		Date:   time.Now().Format(layoutDate),
		PageID: page.ID,
	})
	if err != nil {
		return notion.BlockChildrenResponse{}, err
	}

	block := notion.Block{}
	if err := json.Unmarshal(blockData, &block); err != nil {
		return notion.BlockChildrenResponse{}, fmt.Errorf("unmarshal Block: %w", err)
	}

	return f.Client.AppendBlockChildren(context.TODO(), f.FlashbackPageID, []notion.Block{block})
}
