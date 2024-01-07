package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/dstotijn/go-notion"
)

type CollectorConfig struct {
	DatabaseID           string   `yaml:"databaseID"`
	DatabaseQuery        string   `yaml:"databaseQuery"`
	CollectionIDs        []string `yaml:"collectionIDs"`
	CollectDumpID        string   `yaml:"collectDumpID"`
	CollectDumpTextBlock string   `yaml:"collectDumpTextBlock"` // Format https://pkg.go.dev/github.com/dstotijn/go-notion#ParagraphBlock
	// CollectDumpBlock string   `yaml:"collectDumpBlock"` // DEPRECATED (2023-12) use collectDumpTextBlock
}

type Collector struct {
	DebugMode bool

	Client *notion.Client
	CollectorConfig
}

func (c *Collector) Validate() error {
	if len(c.CollectDumpTextBlock) == 0 {
		return errors.Join(ErrConfigRequired, fmt.Errorf("set collectDumpTextBlock"))
	}
	return nil
}

func (c *Collector) Run() error {
	collected := c.GetCollected()
	log.Printf("Found collected pages: %d", len(collected))

	pagesChan, errChan := c.ScanPages()
	pageNum := 0
	newPages := []string{}
	for pages := range pagesChan {
		for _, page := range pages {
			pageNum += 1

			if !collected[page.ID] {
				newPages = append(newPages, page.ID)
			}

			if c.DebugMode && pageNum%500 == 0 {
				log.Printf("Scanned pages: %v so far", pageNum)
			}
		}
	}
	log.Printf("Scanned pages: %v, new pages: %v", pageNum, len(newPages))

	select {
	case err := <-errChan:
		return err
	default:
	}

	errNum := 0
	for _, newPageID := range newPages {
		if _, err := c.WriteBlock(newPageID); err != nil {
			errNum += 1

			log.Printf("Failed to write block with PageID: %v, err: %v", newPageID, err)
		}
	}
	log.Printf("Updated new pages. Succeed: %d, failed: %d", len(newPages)-errNum, errNum)

	return nil
}

func (c *Collector) GetCollected() map[string]bool {
	collected := map[string]bool{}

	scanIDs := c.CollectionIDs
	nextScanIDs := []string{}
	for {
		if c.DebugMode {
			log.Printf("GetCollected ScanIDs: %v", scanIDs)
		}

		for _, blockID := range scanIDs {
			blocks, err := c.GetCollectionBlocks(blockID)
			if err != nil {
				log.Printf("GetCollectionBlocks Failed. ID: %v, Err: %v", blockID, err)
			}

			for _, block := range blocks {
				if block.HasChildren() {
					nextScanIDs = append(nextScanIDs, block.ID())
				}

				switch b := block.(type) {
				case *notion.ParagraphBlock:
					for _, cBlock := range b.RichText {
						if cBlock.Mention != nil && cBlock.Mention.Type == notion.MentionTypePage {
							collected[cBlock.Mention.Page.ID] = true
						}
					}
				case *notion.ToggleBlock:
					for _, cBlock := range b.RichText {
						if cBlock.Mention != nil && cBlock.Mention.Type == notion.MentionTypePage {
							collected[cBlock.Mention.Page.ID] = true
						}
					}
				}
			}
		}

		if len(nextScanIDs) == 0 {
			break
		}

		scanIDs = nextScanIDs
		nextScanIDs = []string{}
	}

	return collected
}

func (c *Collector) GetCollectionBlocks(blockID string) ([]notion.Block, error) {
	pages := []notion.Block{}

	cursor := ""
	for {
		query := &notion.PaginationQuery{StartCursor: cursor}
		resp, err := c.Client.FindBlockChildrenByID(context.TODO(), blockID, query)
		if err != nil {
			return pages, err
		}

		pages = append(pages, resp.Results...)

		if resp.HasMore {
			cursor = *resp.NextCursor
		} else {
			break
		}
	}

	return pages, nil
}

func (c *Collector) ScanPages() (chan []notion.Page, chan error) {
	q := NewDatabaseQuery(c.Client, c.DatabaseID)

	if err := q.SetQuery(c.DatabaseQuery, QueryBuilder{}); err != nil {
		log.Panicf("Invalid query: %v, err: %v", c.DatabaseQuery, err)
	}

	if c.DebugMode {
		log.Printf("DatabaseQuery Filter: %+v", q.Query.Filter)
		log.Printf("DatabaseQuery Sorter: %+v", q.Query.Sorts)
	}

	return q.Go(context.TODO(), 3)
}

func (c *Collector) WriteBlock(pageID string) (notion.BlockChildrenResponse, error) {
	w := NewAppendBlock(c.Client, c.CollectDumpID)

	if err := w.SetBlock("Collector", c.CollectDumpTextBlock, BlockBuilder{
		PageID: pageID,
	}); err != nil {
		return notion.BlockChildrenResponse{}, err
	}

	return w.WriteParagraph(context.TODO())
}
