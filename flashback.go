package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/dstotijn/go-notion"
)

type FlashbackConfig struct {
	DatabaseID         string    `yaml:"databaseID"`
	DatabaseQuery      string    `yaml:"databaseQuery"`
	OldestTimestamp    time.Time `yaml:"oldestTimestamp"`    // Format time.RFC3339 2006-01-02T15:04:05Z07:00
	FlashbackNum       int       `yaml:"flashbackNum"`       // Number of flashback entries
	FlashbackPageID    string    `yaml:"flashbackPageID"`    // Page to write the flashback
	FlashbackJournalID string    `yaml:"flashbackJournalID"` // Use daily journal database ID, this will overwrite FlashbackPageID
	FlashbackTextBlock string    `yaml:"flashbackTextBlock"` // Format https://pkg.go.dev/github.com/dstotijn/go-notion#ParagraphBlock
	// FlashbackPageBlock string    `yaml:"flashbackPageBlock"` // DEPRECATED (2023-12) use flashbackTextBlock
}

type Flashback struct {
	DebugMode bool

	Client *notion.Client
	FlashbackConfig
}

func (f *Flashback) Validate() error {
	if f.FlashbackPageID == "" && f.FlashbackJournalID == "" {
		return errors.Join(ErrConfigRequired, fmt.Errorf("set flashbackPageID or flashbackJournalID"))
	}

	if f.FlashbackTextBlock == "" {
		return errors.Join(ErrConfigRequired, fmt.Errorf("set flashbackTextBlock"))
	}

	return nil
}

func (f *Flashback) Run() error {
	f.SetFlashbackPageID()

	maxHours := int(time.Since(f.OldestTimestamp).Hours())
	lookbackHour := rand.Intn(maxHours)

	pages, err := f.GetPages(time.Duration(lookbackHour) * time.Hour)
	if err != nil {
		return err
	}

	log.Printf("Lookback %v Hours/%v Day, Queried pages: %+v", lookbackHour, lookbackHour/24, len(pages))
	if len(pages) < 1 {
		log.Printf("Skipped. no pages fetched")
		return nil
	}

	if f.FlashbackNum < 1 {
		f.FlashbackNum = 1
	} else if f.FlashbackNum > len(pages) {
		f.FlashbackNum = len(pages)
	}

	skip := map[int]struct{}{}
	for i := 0; i < f.FlashbackNum; i++ {
		n := rand.Intn(len(pages))
		for {
			if _, found := skip[n]; found {
				n = (n + 1) % len(pages)
			} else {
				skip[n] = struct{}{}
				break
			}
		}

		if block, err := f.WriteBlock(pages[n].ID); err == nil {
			if len(block.Results) > 0 {
				log.Printf("Append block child %v", block.Results[0].ID())
			}
		}
	}
	return nil
}

func (f *Flashback) GetPages(lookback time.Duration) ([]notion.Page, error) {
	q := NewDatabaseQuery(f.Client, f.DatabaseID)

	if err := q.SetQuery(f.DatabaseQuery, QueryBuilder{
		Date: time.Now().Add(-lookback).Format(layoutDate),
	}); err != nil {
		log.Panicf("Invalid query: %v, err: %v", f.DatabaseQuery, err)
	}

	if f.DebugMode {
		log.Printf("DatabaseQuery Filter: %+v", q.Query.Filter)
		log.Printf("DatabaseQuery Sorter: %+v", q.Query.Sorts)
	}

	return q.Once(context.TODO())
}

func (f *Flashback) SetFlashbackPageID() {
	if f.FlashbackJournalID == "" {
		return
	}

	now := time.Now()
	title := now.Format(layoutDate)

	q := NewDatabaseQuery(f.Client, f.FlashbackJournalID)

	if err := q.SetQuery(tmplQueryDBbyTitle, QueryBuilder{
		Title: title,
	}); err != nil {
		log.Panicf("Invalid query: %v, err: %v", f.DatabaseQuery, err)
	}

	pages, err := q.Once(context.TODO())
	if err != nil {
		log.Panicf("No journal found: %v, err: %v", title, err)
	}

	if len(pages) > 1 {
		log.Printf("Multiple journal found: %v, cnt: %v, uses: %v", title, len(pages), pages[0].ID)
	}

	if f.DebugMode {
		log.Printf("Journal by title: %v, found: %v, uses: %v", title, len(pages), pages[0].ID)
	}

	f.FlashbackPageID = pages[0].ID
}

func (f *Flashback) WriteBlock(pageID string) (notion.BlockChildrenResponse, error) {
	w := NewAppendBlock(f.Client, f.FlashbackPageID)

	if err := w.AddParagraph("Flashback", f.FlashbackTextBlock, BlockBuilder{
		Date:   time.Now().Format(layoutDate),
		PageID: pageID,
	}); err != nil {
		return notion.BlockChildrenResponse{}, err
	}

	return w.Do(context.TODO())
}
