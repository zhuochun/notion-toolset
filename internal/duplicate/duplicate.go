package duplicate

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/notionops"
	"github.com/zhuochun/notion-toolset/transformer"
)

type DuplicateChecker struct {
	Logger    *log.Logger
	DebugMode bool

	Client *notion.Client
	config.DuplicateCheckerConfig
}

func (d *DuplicateChecker) Validate() error {
	if len(d.DuplicateDumpTextBlock) == 0 {
		return errors.Join(config.ErrRequired, fmt.Errorf("set duplicateDumpTextBlock"))
	}
	return nil
}

func (d *DuplicateChecker) Run() error {
	pageNum := 0
	set := map[string]string{}
	reported := map[string]struct{}{}
	err := d.ScanPages(context.TODO(), func(page notion.Page) error {
		pageNum++
		keys := d.pageKeys(page)
		if len(keys) != 0 {
			for _, key := range keys {
				if id, ok := set[key]; ok {
					if err := d.reportDuplicatePage(reported, id); err != nil {
						return err
					}
					if err := d.reportDuplicatePage(reported, page.ID); err != nil {
						return err
					}
				} else {
					set[key] = page.ID
				}
			}
		}

		if d.brokenURLCheck(page) {
			if err := d.reportDuplicatePage(reported, page.ID); err != nil {
				return err
			}
		}

		if d.DebugMode && pageNum%500 == 0 {
			d.logger().Printf("Scanned pages: %v so far", pageNum)
		}
		return nil
	})
	d.logger().Printf("Scanned pages: %v, unique keys: %v", pageNum, len(set))
	return err
}

func (d *DuplicateChecker) reportDuplicatePage(reported map[string]struct{}, pageID string) error {
	if _, ok := reported[pageID]; ok {
		return nil
	}
	if _, err := d.WriteBlock(pageID); err != nil {
		return err
	}
	reported[pageID] = struct{}{}
	return nil
}

func (d *DuplicateChecker) ScanPages(ctx context.Context, visit func(notion.Page) error) error {
	q := notionops.NewDatabaseQuery(d.Client, d.DatabaseID)

	if err := q.SetQuery(d.DatabaseQuery, notionops.QueryBuilder{}); err != nil {
		d.logger().Panicf("Invalid query: %v, err: %v", d.DatabaseQuery, err)
	}

	if d.DebugMode {
		d.logger().Printf("DatabaseQuery Filter: %+v", q.Query.Filter)
		d.logger().Printf("DatabaseQuery Sorter: %+v", q.Query.Sorts)
	}

	return q.ForEach(ctx, 0, nil, visit)
}

func (d *DuplicateChecker) WriteBlock(pageID string) (notion.BlockChildrenResponse, error) {
	w := notionops.NewAppendBlock(d.Client, d.DuplicateDumpID)

	if err := w.AddParagraph("Duplicate", d.DuplicateDumpTextBlock, notionops.BlockBuilder{
		Date:   time.Now().Format(notionops.DateLayout),
		PageID: pageID,
	}); err != nil {
		return notion.BlockChildrenResponse{}, err
	}

	return w.Do(context.TODO())
}

// pageKeys returns the set of keys for duplicate detection. When no
// CheckProperties are configured, the page title is used. Keys with empty
// values are omitted.
func (d *DuplicateChecker) pageKeys(page notion.Page) []string {
	if len(d.CheckProperties) == 0 {
		title, err := transformer.GetPageTitle(page)
		if err != nil {
			d.logger().Printf("Err pageID: %v, err: %v", page.ID, err)
			return nil
		}
		if title == "" {
			return nil
		}
		return []string{"title=" + title}
	}

	props, ok := page.Properties.(notion.DatabasePageProperties)
	if !ok {
		title, err := transformer.GetPageTitle(page)
		if err != nil {
			d.logger().Printf("Err pageID: %v, err: %v", page.ID, err)
			return nil
		}
		if title == "" {
			return nil
		}
		return []string{"title=" + title}
	}

	keys := []string{}
	for _, name := range d.CheckProperties {
		prop, ok := props[name]
		if !ok {
			continue
		}
		val := stringifyDBProp(prop)
		if val != "" {
			keys = append(keys, name+"="+val)
		}
	}
	return keys
}

// stringifyDBProp converts a notion.DatabasePageProperty into a human readable
// value used for duplicate comparison. Unsupported or empty values result in an
// empty string.
func stringifyDBProp(prop notion.DatabasePageProperty) string {
	switch prop.Type {
	case notion.DBPropTypeTitle:
		return transformer.ConcatRichText(prop.Title)
	case notion.DBPropTypeRichText:
		return transformer.ConcatRichText(prop.RichText)
	case notion.DBPropTypeURL:
		if prop.URL != nil {
			return *prop.URL
		}
	}
	return ""
}

func (d *DuplicateChecker) logger() *log.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return log.Default()
}
