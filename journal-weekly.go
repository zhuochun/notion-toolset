package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/transformer"
)

type WeeklyJournalConfig struct {
	DatabaseID     string `yaml:"databaseID"`
	Limit          int    `yaml:"limit"`
	PageQuery      string `yaml:"pageQuery"`
	PageProperties string `yaml:"pageProperties"`
}

type WeeklyJournal struct {
	DebugMode bool

	Client *notion.Client
	WeeklyJournalConfig
}

func (d *WeeklyJournal) Validate() error {
	return nil
}

func (d *WeeklyJournal) Run() error {
	now := time.Now()
	tCursor := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	pages, err := d.GetPages(tCursor)
	if err != nil {
		return err
	}

	if d.DebugMode {
		log.Printf("Pages found: %v", pages)
	}

	for i := 0; i < d.Limit; i++ {
		tCursor = d.NextMonday(tCursor)
		tSunday := tCursor.AddDate(0, 0, 6)
		title := tCursor.Format(layoutDate) + "/" + tSunday.Format(layoutDate)

		if pages[title] {
			continue
		}

		page, err := d.CreatePage(title, tCursor, tSunday)
		if err != nil {
			log.Printf("Create Page `%v` met Error: %v", title, err)
			continue
		}
		log.Printf("Created page `%v` with ID: %v", title, page.ID)
	}

	return nil
}

func (d *WeeklyJournal) NextMonday(tCursor time.Time) time.Time {
	if tCursor.Weekday() == time.Sunday {
		return tCursor.AddDate(0, 0, 1)
	} else {
		return tCursor.AddDate(0, 0, 8-int(tCursor.Weekday()))
	}
}

func (d *WeeklyJournal) GetPages(tCursor time.Time) (map[string]bool, error) {
	queryData, err := Tmpl("DatabaseQuery", d.PageQuery, QueryBuilder{
		Date: tCursor.Format(layoutDate),
	})
	if err != nil {
		return nil, err
	}

	query := &notion.DatabaseQuery{}
	if err := json.Unmarshal(queryData, query); err != nil {
		return nil, fmt.Errorf("unmarshal DatabaseQuery: %w", err)
	}

	if d.DebugMode {
		log.Printf("DatabaseQuery Filter: %+v", query.Filter)
		log.Printf("DatabaseQuery Sorter: %+v", query.Sorts)
	}

	resp, err := d.Client.QueryDatabase(context.TODO(), d.DatabaseID, query)
	if err != nil {
		return nil, err
	}

	pages := map[string]bool{}
	for _, page := range resp.Results {
		title, err := transformer.GetPageTitle(page)
		if err != nil {
			return nil, fmt.Errorf("invalid DatabaseQuery response: %w", err)
		}
		pages[title] = true
	}
	return pages, nil
}

func (d *WeeklyJournal) CreatePage(title string, date, dateEnd time.Time) (notion.Page, error) {
	propData, err := Tmpl("CreatePage Properties", d.PageProperties, PageBuilder{
		Title:      title,
		Date:       date.Format(layoutDate),
		DateEnd:    dateEnd.Format(layoutDate),
		DatabaseID: d.DatabaseID,
	})
	if err != nil {
		return notion.Page{}, err
	}

	props := &notion.DatabasePageProperties{}
	if err := json.Unmarshal(propData, props); err != nil {
		return notion.Page{}, fmt.Errorf("unmarshal Page properties: %w", err)
	}

	if d.DebugMode {
		log.Printf("Page properties: %+v", props)
	}

	return d.Client.CreatePage(context.TODO(), notion.CreatePageParams{
		ParentType:             notion.ParentTypeDatabase,
		ParentID:               d.DatabaseID,
		DatabasePageProperties: props,
	})
}
