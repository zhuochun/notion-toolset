package collector

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/notionops"
	"github.com/zhuochun/notion-toolset/notionread"
)

type Collector struct {
	Logger    *log.Logger
	DebugMode bool

	Client *notion.Client
	config.CollectorConfig
}

const collectorWriteBatchSize = 100

func (c *Collector) Validate() error {
	if len(c.CollectDumpTextBlock) == 0 {
		return errors.Join(config.ErrRequired, fmt.Errorf("set collectDumpTextBlock"))
	}
	return nil
}

func (c *Collector) Run() error {
	collected, err := c.GetCollected()
	if err != nil {
		return fmt.Errorf("get collected pages: %w", err)
	}
	c.logger().Printf("Found collected pages: %d", len(collected))

	pageNum := 0
	newPages := []string{}
	err = c.ScanPages(context.TODO(), func(page notion.Page) error {
		pageNum++
		if !collected[page.ID] {
			newPages = append(newPages, page.ID)
		}
		if c.DebugMode && pageNum%500 == 0 {
			c.logger().Printf("Scanned pages: %v so far", pageNum)
		}
		return nil
	})
	c.logger().Printf("Scanned pages: %v, new pages: %v", pageNum, len(newPages))
	if err != nil {
		return err
	}

	succeeded, failed := c.WriteBlocks(newPages)
	c.logger().Printf("Updated new pages. Succeed: %d, failed: %d", succeeded, failed)

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
	q := notionops.NewDatabaseQuery(c.Client, c.DatabaseID)

	if err := q.SetQuery(c.DatabaseQuery, notionops.QueryBuilder{}); err != nil {
		c.logger().Panicf("Invalid query: %v, err: %v", c.DatabaseQuery, err)
	}

	if c.DebugMode {
		c.logger().Printf("DatabaseQuery Filter: %+v", q.Query.Filter)
		c.logger().Printf("DatabaseQuery Sorter: %+v", q.Query.Sorts)
	}

	return q.ForEach(ctx, 0, nil, visit)
}

func (c *Collector) WriteBlocks(pageIDs []string) (succeeded, failed int) {
	for start := 0; start < len(pageIDs); start += collectorWriteBatchSize {
		end := min(start+collectorWriteBatchSize, len(pageIDs))
		batch := pageIDs[start:end]
		w := notionops.NewAppendBlock(c.Client, c.CollectDumpID)
		batchReady := true

		for _, pageID := range batch {
			if err := w.AddParagraph("Collector", c.CollectDumpTextBlock, notionops.BlockBuilder{
				PageID: pageID,
			}); err != nil {
				failed += len(batch)
				batchReady = false
				c.logger().Printf("Failed to build collection block batch. Pages: %d, first PageID: %v, err: %v", len(batch), batch[0], err)
				break
			}
		}
		if !batchReady {
			continue
		}
		if _, err := w.Do(context.TODO()); err != nil {
			failed += len(batch)
			c.logger().Printf("Failed to write collection block batch. Pages: %d, first PageID: %v, err: %v", len(batch), batch[0], err)
			continue
		}

		succeeded += len(batch)
	}

	return succeeded, failed
}

func (c *Collector) logger() *log.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return log.Default()
}
