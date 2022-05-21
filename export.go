package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/transformer"
	"golang.org/x/time/rate"
)

type ExporterConfig struct {
	DatabaseID    string `yaml:"databaseID"`
	DatabaseQuery string `yaml:"databaseQuery"`
	// export related
	LookbackDays       int      `yaml:"lookbackDays"` // leave this empty for full backup
	Directory          string   `yaml:"directory"`    // output directory
	UseTitleAsFilename bool     `yaml:"useTitleAsFilename"`
	ReplaceTitle       []string `yaml:"replaceTitle"`
	// transformer
	Markdown transformer.MarkdownConfig `yaml:"markdown"`
	// tuning https://developers.notion.com/reference/request-limits
	ExportSpeed float64 `yaml:"exportSpeed"`
	// debug
	DebugLimit int  `yaml:"debugLimit"`
	DebugCache bool `yaml:"debugCache"`
}

type Exporter struct {
	DebugMode bool

	Client *notion.Client
	ExporterConfig

	queryLimiter *rate.Limiter
	queryPool    chan *transformer.BlockFuture
	exportPool   chan notion.Page
}

func (e *Exporter) Run() error {
	if err := e.precheck(); err != nil {
		return err
	}

	e.queryLimiter = rate.NewLimiter(rate.Limit(e.ExportSpeed), int(e.ExportSpeed))
	queryWg := new(sync.WaitGroup)
	e.queryPool = e.StartWorker(queryWg, int(e.ExportSpeed))

	exportWg := new(sync.WaitGroup)
	e.exportPool = e.StartExporter(exportWg, int(e.ExportSpeed))

	pagesChan, errChan := e.ScanPages()
	pageNum := 0
	for pages := range pagesChan {
		for _, page := range pages {
			pageNum += 1
			e.exportPool <- page

			if e.DebugMode && pageNum%500 == 0 {
				log.Printf("Scanned pages: %v so far", pageNum)
			}
		}
	}
	log.Printf("Scanned pages: %v", pageNum)

	close(e.exportPool)
	exportWg.Wait()

	close(e.queryPool)
	queryWg.Wait()

	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}

func (e *Exporter) precheck() error {
	// check export directory
	if pathInfo, err := os.Stat(e.Directory); err == nil {
		if !pathInfo.IsDir() {
			return fmt.Errorf("directory is invalid: %v", e.Directory)
		}
	} else {
		return fmt.Errorf("directory does not exists: %v", e.Directory)
	}

	// set default exportspeed
	if e.ExportSpeed < 1 {
		e.ExportSpeed = 2.8
	} else if e.ExportSpeed > 3 {
		e.ExportSpeed = 3
	}

	return nil
}

func (e *Exporter) ScanPages() (chan []notion.Page, chan error) {
	q := NewDatabaseQuery(e.Client, e.DatabaseID)

	date := "" // default
	if e.LookbackDays > 0 {
		date = time.Now().AddDate(0, 0, -e.LookbackDays).Format(layoutDate)
	}

	if err := q.SetQuery(e.DatabaseQuery, QueryBuilder{Date: date}); err != nil {
		log.Panicf("Invalid query: %v, err: %v", e.DatabaseQuery, err)
	}

	if e.DebugLimit > 0 {
		q.Query.PageSize = e.DebugLimit
	}

	if e.DebugMode {
		log.Printf("DatabaseQuery Filter: %+v", q.Query.Filter)
		log.Printf("DatabaseQuery Sorter: %+v", q.Query.Sorts)
	}

	return q.Go(context.Background(), 1, e.queryLimiter)
}

func (e *Exporter) StartWorker(wg *sync.WaitGroup, size int) chan *transformer.BlockFuture {
	taskPool := make(chan *transformer.BlockFuture, size)

	for i := 0; i < size; i++ {
		wg.Add(1)

		go func() {
			for task := range taskPool {
				blocks, err := e.QueryBlocks(task.BlockID)
				task.Write(blocks, err)
			}
			wg.Done()
		}()
	}

	return taskPool
}

func (e *Exporter) QueryBlocks(blockID string) ([]notion.Block, error) {
	e.queryLimiter.Wait(context.Background())

	blocks := []notion.Block{}
	cursor := ""
	for {
		query := &notion.PaginationQuery{StartCursor: cursor}
		resp, err := e.Client.FindBlockChildrenByID(context.TODO(), blockID, query)
		if err != nil {
			return blocks, err
		}

		blocks = append(blocks, resp.Results...)

		if resp.HasMore {
			cursor = *resp.NextCursor
		} else {
			break
		}
	}

	if e.DebugCache {
		e.writeDebugCache(blockID, blocks)
	}
	return blocks, nil
}

func (e *Exporter) writeDebugCache(id string, v interface{}) {
	filename := "temp/" + id + ".json"
	file, err := os.Create(filename)
	if err != nil {
		log.Printf("Failed to create debug cache, name: %v, err: %v", filename, err)
		return
	}

	c, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal debug cache, name: %v, err: %v", filename, err)
		return
	}

	file.Write(c)
	file.Close()
}

func (e *Exporter) StartExporter(wg *sync.WaitGroup, size int) chan notion.Page {
	taskPool := make(chan notion.Page, size)

	for i := 0; i < size; i++ {
		wg.Add(1)

		go func() {
			for page := range taskPool {
				if e.DebugCache {
					e.writeDebugCache("page-"+page.ID, page)
				}

				// get content in the page
				blocks, err := e.QueryBlocks(page.ID)
				if err != nil {
					log.Printf("Failed to query blocks, id: %v, err: %v", page.ID, err)
					continue
				}

				filename := e.createFilename(page)
				// create output file
				file, err := os.Create(filename)
				if err != nil {
					log.Printf("Failed to create new file, name: %v, err: %v", filename, err)
					continue
				}

				// transform from block to the file format
				t := transformer.New(e.Markdown, &page, blocks, e.queryPool)
				t.TransformOut(file)

				// close file in the loop
				file.Close()

				if e.DebugMode {
					log.Printf("Finished exporting one file, name: %v", filename)
				}
			}
			wg.Done()
		}()
	}

	return taskPool
}

func (e *Exporter) createFilename(page notion.Page) string {
	filename := e.Directory + transformer.SimpleID(page.ID) + ".md"
	// TODO slug the title?
	if e.UseTitleAsFilename {
		if title, err := transformer.GetPageTitle(page); err == nil {
			if len(e.ReplaceTitle) == 2 {
				title = strings.ReplaceAll(title, e.ReplaceTitle[0], e.ReplaceTitle[1])
			}
			title = strings.TrimSpace(title)

			filename = e.Directory + title + ".md"
		}
	}
	return filename
}
