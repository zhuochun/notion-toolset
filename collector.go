package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/notionread"
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

	pageNum := 0
	newPages := []string{}
	err = c.ScanPages(context.TODO(), func(page notion.Page) error {
		pageNum++
		if !collected[page.ID] {
			newPages = append(newPages, page.ID)
		}
		if c.DebugMode && pageNum%500 == 0 {
			log.Printf("Scanned pages: %v so far", pageNum)
		}
		return nil
	})
	log.Printf("Scanned pages: %v, new pages: %v", pageNum, len(newPages))
	if err != nil {
		return err
	}

	succeeded, failed := c.WriteBlocks(newPages)
	log.Printf("Updated new pages. Succeed: %d, failed: %d", succeeded, failed)

	return nil
}

func (c *Collector) GetCollected() (map[string]bool, error) {
	collected := map[string]bool{}
	snapshot, err := notionread.New(c.Client).BlockForest(context.TODO(), c.CollectionIDs, notionread.Strict)
	if err != nil {
		return nil, fmt.Errorf("get collection blocks: %w", err)
	}

	for _, blockID := range snapshot.LoadedBlockIDs() {
		blocks, err := snapshot.Children(blockID)
		if err != nil {
			return nil, fmt.Errorf("get collection blocks %s: %w", blockID, err)
		}

		for _, block := range blocks {
			var richText []notion.RichText
			switch b := block.(type) {
			case *notion.ParagraphBlock:
				richText = b.RichText
			case *notion.ToggleBlock:
				richText = b.RichText
			}
			for _, content := range richText {
				if content.Mention != nil && content.Mention.Type == notion.MentionTypePage {
					collected[content.Mention.Page.ID] = true
				}
			}
		}
	}

	return collected, nil
}

func (c *Collector) ScanPages(ctx context.Context, visit func(notion.Page) error) error {
	q := NewDatabaseQuery(c.Client, c.DatabaseID)

	if err := q.SetQuery(c.DatabaseQuery, QueryBuilder{}); err != nil {
		log.Panicf("Invalid query: %v, err: %v", c.DatabaseQuery, err)
	}

	if c.DebugMode {
		log.Printf("DatabaseQuery Filter: %+v", q.Query.Filter)
		log.Printf("DatabaseQuery Sorter: %+v", q.Query.Sorts)
	}

	return q.ForEach(ctx, 0, nil, visit)
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
