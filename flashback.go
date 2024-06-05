package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
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
	FlashbackChainFile string    `yaml:"flashbackChainFile"` // Filename for chain with LLM cmd
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
	// use a random hour to lookback
	lookbackHour := rand.Intn(maxHours)
	pages, err := f.GetPages(time.Duration(lookbackHour) * time.Hour)
	if err != nil {
		return err
	}
	log.Printf("Lookback %v Hours/%v Day, Queried pages: %+v", lookbackHour, lookbackHour/24, len(pages))

	if len(pages) < 1 { // try again with max hours
		lookbackHour = maxHours
		pages, err = f.GetPages(time.Duration(lookbackHour) * time.Hour)
		if err != nil {
			return err
		}
		log.Printf("Lookback (max) %v Hours/%v Day, Queried pages: %+v", lookbackHour, lookbackHour/24, len(pages))
	}

	if len(pages) < 1 { // give up
		log.Printf("Skipped. no pages fetched")
		return nil
	}

	if f.FlashbackNum < 1 {
		f.FlashbackNum = 1
	} else if f.FlashbackNum > len(pages) {
		f.FlashbackNum = len(pages)
	}

	picked := map[int]struct{}{}
	for i := 0; i < f.FlashbackNum; i++ {
		n := rand.Intn(len(pages))
		for {
			if _, found := picked[n]; found {
				n = (n + 1) % len(pages)
			} else {
				picked[n] = struct{}{}
				break
			}
		}
	}

	for n := range picked { // write out block
		if block, err := f.WriteBlock(pages[n].ID); err == nil {
			if len(block.Results) > 0 {
				log.Printf("Append block child %v", block.Results[0].ID())
			}
		}
	}

	if f.FlashbackChainFile != "" { // write out chain file
		file, err := os.Create(f.FlashbackChainFile)
		if err != nil {
			return fmt.Errorf("create file: %v, err: %v", f.FlashbackChainFile, err)
		}
		defer file.Close()

		for n := range picked {
			if _, err = file.WriteString(pages[n].ID + "\n"); err != nil {
				log.Printf("Failed writing to file, err: %v", err)
			}
		}
	}

	return nil
}

func (f *Flashback) GetPages(lookback time.Duration) ([]notion.Page, error) {
	q := NewDatabaseQuery(f.Client, f.DatabaseID)

	if err := q.SetQuery(f.DatabaseQuery, QueryBuilder{
		Date:  time.Now().Add(-lookback).Format(layoutDate),
		Today: time.Now().Format(layoutDate),
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
