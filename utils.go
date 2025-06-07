package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"time"

	"github.com/dstotijn/go-notion"
)

type PageBuilder struct {
	Title      string
	Date       string
	DateEnd    string
	DatabaseID string
}

func Tmpl(name, s string, builder interface{}) ([]byte, error) {
	tmpl, err := template.New(name).Parse(s)
	if err != nil {
		return nil, fmt.Errorf("template %s parse: %w", name, err)
	}

	var raw bytes.Buffer
	if err = tmpl.Execute(&raw, builder); err != nil {
		return nil, fmt.Errorf("template %s execute: %w", name, err)
	}

	return raw.Bytes(), nil
}

type BlockBuilder struct {
	Date    string
	Content string
	PageID  string
}

type AppendBlock struct {
	Client         *notion.Client
	AppendToPageID string

	Blocks []notion.Block
}

func NewAppendBlock(c *notion.Client, appendTo string) *AppendBlock {
	return &AppendBlock{
		Client:         c,
		AppendToPageID: appendTo,
	}
}

// https://pkg.go.dev/github.com/dstotijn/go-notion#ParagraphBlock
// https://pkg.go.dev/github.com/dstotijn/go-notion#RichText
func (a *AppendBlock) AddParagraph(name, s string, builder interface{}) error {
	if s == "" {
		return fmt.Errorf("empty block text: %s", name)
	}

	rawBlock, err := Tmpl(name, s, builder)
	if err != nil {
		return err
	}

	block := &notion.ParagraphBlock{}
	if err := json.Unmarshal(rawBlock, block); err != nil {
		return fmt.Errorf("unmarshal block: %w", err)
	}

	a.Blocks = append(a.Blocks, block)
	return nil
}

func (a *AppendBlock) AddBlocks(name, s string, builder interface{}) error {
	if s == "" {
		return fmt.Errorf("empty block text: %s", name)
	}

	rawBlocks, err := Tmpl(name, s, builder)
	if err != nil {
		return err
	}

	blocks := []notion.ParagraphBlock{}
	if err := json.Unmarshal(rawBlocks, &blocks); err != nil {
		return fmt.Errorf("unmarshal blocks: %w", err)
	}

	for _, b := range blocks {
		a.Blocks = append(a.Blocks, b)
	}
	return nil
}

func (a *AppendBlock) Do(ctx context.Context) (notion.BlockChildrenResponse, error) {
	return a.Client.AppendBlockChildren(ctx, a.AppendToPageID, a.Blocks)
}

// GetTodayJournalPage finds today's journal page by title in the given database.
// The title must match layoutDate (YYYY-MM-DD). When multiple pages are found,
// the first one is returned and a message is logged.
func GetTodayJournalPage(ctx context.Context, c *notion.Client, databaseID string, debug bool) (notion.Page, error) {
	title := time.Now().Format(layoutDate)

	q := NewDatabaseQuery(c, databaseID)
	q.Query = &notion.DatabaseQuery{
		Filter: &notion.DatabaseQueryFilter{
			Property: "title",
			DatabaseQueryPropertyFilter: notion.DatabaseQueryPropertyFilter{
				Title: &notion.TextPropertyFilter{Equals: title},
			},
		},
		Sorts: []notion.DatabaseQuerySort{{Timestamp: notion.SortTimeStampCreatedTime, Direction: notion.SortDirAsc}},
	}

	pages, err := q.Once(ctx)
	if err != nil {
		return notion.Page{}, fmt.Errorf("no journal found: %v, err: %w", title, err)
	}
	if len(pages) == 0 {
		return notion.Page{}, fmt.Errorf("no journal found: %v", title)
	}
	if len(pages) > 1 {
		log.Printf("Multiple journal found: %v, cnt: %v, uses: %v", title, len(pages), pages[0].ID)
	}
	if debug {
		log.Printf("Journal by title: %v, found: %v, uses: %v", title, len(pages), pages[0].ID)
	}

	return pages[0], nil
}
