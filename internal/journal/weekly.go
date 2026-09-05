package journal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/notionops"
	"github.com/zhuochun/notion-toolset/transformer"
)

type WeeklyJournal struct {
	Now       func() time.Time
	Logger    *log.Logger
	DebugMode bool

	Client *notion.Client
	config.WeeklyJournalConfig
}

func (d *WeeklyJournal) Validate() error {
	return nil
}

func (d *WeeklyJournal) Run() error {
	now := d.now()
	tCursor := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	pages, err := d.GetPages(tCursor)
	if err != nil {
		return err
	}

	if d.DebugMode {
		d.logger().Printf("Pages found: %v", pages)
	}

	for i := 0; i < d.Limit; i++ {
		tCursor = d.NextMonday(tCursor)
		tSunday := tCursor.AddDate(0, 0, 6)
		title := tCursor.Format(notionops.DateLayout) + "/" + tSunday.Format(notionops.DateLayout)

		if pages[title] {
			continue
		}

		page, err := d.CreatePage(title, tCursor, tSunday)
		if err != nil {
			d.logger().Printf("Create Page `%v` met Error: %v", title, err)
			continue
		}
		d.logger().Printf("Created page `%v` with ID: %v", title, page.ID)
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
	q := notionops.NewDatabaseQuery(d.Client, d.DatabaseID)
	if err := q.SetQuery(d.PageQuery, notionops.QueryBuilder{
		Date: tCursor.Format(notionops.DateLayout),
	}); err != nil {
		return nil, err
	}

	if d.DebugMode {
		d.logger().Printf("DatabaseQuery Filter: %+v", q.Query.Filter)
		d.logger().Printf("DatabaseQuery Sorter: %+v", q.Query.Sorts)
	}

	results, err := q.Once(context.TODO())
	if err != nil {
		return nil, err
	}

	pages := map[string]bool{}
	for _, page := range results {
		title, err := transformer.GetPageTitle(page)
		if err != nil {
			return nil, fmt.Errorf("invalid DatabaseQuery response: %w", err)
		}
		pages[title] = true
	}
	return pages, nil
}

func (d *WeeklyJournal) CreatePage(title string, date, dateEnd time.Time) (notion.Page, error) {
	propData, err := notionops.Tmpl("CreatePage Properties", d.PageProperties, notionops.PageBuilder{
		Title:      title,
		Date:       date.Format(notionops.DateLayout),
		DateEnd:    dateEnd.Format(notionops.DateLayout),
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
		d.logger().Printf("Page properties: %+v", props)
	}

	return d.Client.CreatePage(context.TODO(), notion.CreatePageParams{
		ParentType:             notion.ParentTypeDatabase,
		ParentID:               d.DatabaseID,
		DatabasePageProperties: props,
	})
}

func (d *WeeklyJournal) logger() *log.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return log.Default()
}

func (d *WeeklyJournal) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}
