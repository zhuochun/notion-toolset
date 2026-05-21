package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/transformer"
)

type DuplicateCheckerConfig struct {
	DatabaseID    string `yaml:"databaseID"`
	DatabaseQuery string `yaml:"databaseQuery"`
	// CheckProperties specifies property names used to detect duplicates.
	// A page is considered a duplicate when any of the listed property
	// values matches another page's value (OR semantics). If the slice is
	// empty, page titles are used. Empty or nil property values are ignored.
	CheckProperties        []string `yaml:"checkProperties"`
	BrokenURLProperty      string   `yaml:"brokenURLproperty"`
	DuplicateDumpID        string   `yaml:"duplicateDumpID"`
	DuplicateDumpTextBlock string   `yaml:"duplicateDumpTextBlock"` // Format https://pkg.go.dev/github.com/dstotijn/go-notion#ParagraphBlock
	// DuplicateDumpBlock string   `yaml:"duplicateDumpBlock"` // DEPRECATED (2023-12) use duplicateDumpTextBlock
}

type DuplicateChecker struct {
	DebugMode bool

	Client *notion.Client
	DuplicateCheckerConfig
}

var duplicateURLHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
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
	reported := map[string]struct{}{}
	for pages := range pagesChan {
		for _, page := range pages {
			pageNum += 1

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
				log.Printf("Scanned pages: %v so far", pageNum)
			}
		}
	}
	log.Printf("Scanned pages: %v, unique keys: %v", pageNum, len(set))

	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
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

	if err := w.AddParagraph("Duplicate", d.DuplicateDumpTextBlock, BlockBuilder{
		Date:   time.Now().Format(layoutDate),
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
			log.Printf("Err pageID: %v, err: %v", page.ID, err)
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
			log.Printf("Err pageID: %v, err: %v", page.ID, err)
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

// brokenURLCheck checks if the configured URL property of the page is broken.
// It returns true when the URL is set but leads to a non-OK HTTP status or
// errors during request. When BrokenURLProperty is empty, it always returns
// false.
func (d *DuplicateChecker) brokenURLCheck(page notion.Page) bool {
	if d.BrokenURLProperty == "" {
		return false
	}

	props, ok := page.Properties.(notion.DatabasePageProperties)
	if !ok {
		return false
	}

	prop, ok := props[d.BrokenURLProperty]
	if !ok {
		return false
	}

	urlStr := stringifyDBProp(prop)
	if urlStr == "" {
		return false
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodHead, urlStr, nil)
	if err != nil {
		return true
	}
	resp, err := duplicateURLHTTPClient.Do(req)
	if err != nil || resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		getReq, getErr := http.NewRequestWithContext(context.Background(), http.MethodGet, urlStr, nil)
		if getErr != nil {
			return true
		}
		getResp, getErr := duplicateURLHTTPClient.Do(getReq)
		if getErr != nil {
			return true
		}
		defer getResp.Body.Close()
		return getResp.StatusCode >= http.StatusBadRequest
	}
	if err != nil {
		return true
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return true
	}
	return false
}
