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

const collectorWriteBatchSize = 100

func (c *Collector) Validate() error {
	if len(c.CollectDumpTextBlock) == 0 {
		return errors.Join(ErrConfigRequired, fmt.Errorf("set collectDumpTextBlock"))
	}
	return nil
}

func (c *Collector) Run() error {
	collected, err := c.GetCollected()
	if err != nil {
		return fmt.Errorf("get collected pages: %w", err)
	}
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

	succeeded, failed := c.WriteBlocks(newPages)
	log.Printf("Updated new pages. Succeed: %d, failed: %d", succeeded, failed)

	return nil
}

func (c *Collector) GetCollected() (map[string]bool, error) {
	collected := map[string]bool{}
	visited := map[string]struct{}{}

	scanIDs := c.CollectionIDs
	nextScanIDs := []string{}
	for {
		if c.DebugMode {
			log.Printf("GetCollected ScanIDs: %v", scanIDs)
		}

		for _, blockID := range scanIDs {
			if _, ok := visited[blockID]; ok {
				continue
			}
			visited[blockID] = struct{}{}

			blocks, err := c.GetCollectionBlocks(blockID)
			if err != nil {
				return nil, fmt.Errorf("get collection blocks %s: %w", blockID, err)
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

	return collected, nil
}

func (c *Collector) GetCollectionBlocks(blockID string) ([]notion.Block, error) {
	pages := []notion.Block{}

	cursor := ""
	for {
		query := &notion.PaginationQuery{StartCursor: cursor}
		var resp notion.BlockChildrenResponse
		err := retryNotion(func() error {
			var innerErr error
			resp, innerErr = c.Client.FindBlockChildrenByID(context.TODO(), blockID, query)
			return innerErr
		})
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

func (c *Collector) WriteBlocks(pageIDs []string) (succeeded, failed int) {
	for start := 0; start < len(pageIDs); start += collectorWriteBatchSize {
		end := min(start+collectorWriteBatchSize, len(pageIDs))
		batch := pageIDs[start:end]
		w := NewAppendBlock(c.Client, c.CollectDumpID)
		batchReady := true

		for _, pageID := range batch {
			if err := w.AddParagraph("Collector", c.CollectDumpTextBlock, BlockBuilder{
				PageID: pageID,
			}); err != nil {
				failed += len(batch)
				batchReady = false
				log.Printf("Failed to build collection block batch. Pages: %d, first PageID: %v, err: %v", len(batch), batch[0], err)
				break
			}
		}
		if !batchReady {
			continue
		}
		if _, err := w.Do(context.TODO()); err != nil {
			failed += len(batch)
			log.Printf("Failed to write collection block batch. Pages: %d, first PageID: %v, err: %v", len(batch), batch[0], err)
			continue
		}

		succeeded += len(batch)
	}

	return succeeded, failed
}
