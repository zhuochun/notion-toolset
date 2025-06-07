package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/retry"
	"github.com/zhuochun/notion-toolset/transformer"
	"golang.org/x/time/rate"
)

type ExporterConfig struct {
	DatabaseID    string `yaml:"databaseID"`
	DatabaseQuery string `yaml:"databaseQuery"`
	// export related
	LookbackDays       int      `yaml:"lookbackDays"`   // leave this empty for full backup
	Directory          string   `yaml:"directory"`      // output directory
	AssetDirectory     string   `yaml:"assetDirectory"` // output directory for assets (images, etc)
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
	ExecOne   string

	Client *notion.Client
	ExporterConfig

	queryLimiter *rate.Limiter

	exportPool   chan notion.Page
	queryPool    chan *transformer.BlockFuture
	downloadPool chan *transformer.AssetFuture
}

func (e *Exporter) Validate() error {
	// handle execOne special case
	if e.ExecOne != "" {
		if e.Directory == "" { // assume current directory
			e.Directory, _ = os.Getwd()
		}

		if reflect.DeepEqual(e.Markdown, transformer.MarkdownConfig{}) { // set to sensible defaults
			e.Markdown = transformer.MarkdownConfig{
				NoAlias:        true,
				NoFrontMatters: true,
				NoMetadata:     true,
				TitleToH1:      true,
				PlainText:      true,
			}
		}
	}

	// check export directory
	if err := e.precheckDir(e.Directory); err != nil {
		return err
	}

	// check asset directory
	if e.AssetDirectory != "" {
		if err := e.precheckDir(e.AssetDirectory); err != nil {
			return err
		}
	}

	// set default exportspeed
	if e.ExportSpeed < 1 {
		e.ExportSpeed = 2.8
	} else if e.ExportSpeed > 3 {
		e.ExportSpeed = 3
	}

	return nil
}

func (e *Exporter) precheckDir(dir string) error {
	pathInfo, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("directory does not exists: %v. Create it first", dir)
	}

	if !pathInfo.IsDir() {
		return fmt.Errorf("directory is invalid: %v", dir)
	}
	return nil
}

func (e *Exporter) Run() error {
	e.queryLimiter = rate.NewLimiter(rate.Limit(e.ExportSpeed), int(e.ExportSpeed))

	// workers to write markdowns
	exportWg := new(sync.WaitGroup)
	e.exportPool = e.StartExporter(exportWg, int(e.ExportSpeed))
	// workers to query content of notion blocks
	queryWg := new(sync.WaitGroup)
	e.queryPool = e.StartQuerier(queryWg, int(e.ExportSpeed))
	// workers to download assets
	downloadWg := new(sync.WaitGroup)
	e.downloadPool = e.StartDownloader(downloadWg, int(e.ExportSpeed)*2)

	// query database pages, queue each pages for export
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

	close(e.downloadPool)
	downloadWg.Wait()

	close(e.queryPool)
	queryWg.Wait()

	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}

func (e *Exporter) ScanPages() (chan []notion.Page, chan error) {
	if e.ExecOne != "" {
		pagesChan := make(chan []notion.Page, 1)
		errChan := make(chan error, 1)

		if page, err := e.findPageByIDWithRetry(context.Background(), e.ExecOne); err == nil {
			pagesChan <- []notion.Page{page}
		} else {
			errChan <- err
		}

		close(pagesChan)
		return pagesChan, errChan
	}

	return e.scanDatabasePages()
}

func (e *Exporter) scanDatabasePages() (chan []notion.Page, chan error) {
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

func (e *Exporter) StartQuerier(wg *sync.WaitGroup, size int) chan *transformer.BlockFuture {
	taskPool := make(chan *transformer.BlockFuture, size)

	for i := 0; i < size; i++ {
		wg.Add(1)

		go func() {
			for task := range taskPool {
				blocks, err := e.QueryBlocks(task.BlockID) // TODO add retry?
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
		resp, err := e.findBlockChildrenByIDWithRetry(context.TODO(), blockID, query)
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
				if err := e.exportPage(page); err != nil {
					log.Printf("Failed to export: %v", err)
				}
			}

			wg.Done()
		}()
	}

	return taskPool
}

func (e *Exporter) exportPage(page notion.Page) error {
	if e.DebugCache {
		e.writeDebugCache("page-"+page.ID, page)
	}

	blocks, err := e.QueryBlocks(page.ID)
	if err != nil {
		return fmt.Errorf("query block id: %v, err: %v", page.ID, err)
	}

	filename := e.getExportFilename(page)
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("create file: %v, err: %v", filename, err)
	}
	defer file.Close()

	if e.DebugMode {
		log.Printf("Exported to file: [%v] -> %v", page.ID, filename)
	}

	t := transformer.New(e.Markdown, &page, blocks, e.queryPool, e.downloadPool)
	t.TransformOut(file)

	// export sub-pages inside this page
	for _, block := range blocks {
		switch b := block.(type) {
		case *notion.ChildPageBlock:
			if child, err := e.findPageByIDWithRetry(context.Background(), b.ID()); err == nil {
				if err := e.exportPage(child); err != nil {
					log.Printf("Failed to export sub-page: %v", err)
				}
			}
		case *notion.LinkToPageBlock:
			if b.PageID != "" {
				if child, err := e.findPageByIDWithRetry(context.Background(), b.PageID); err == nil {
					if err := e.exportPage(child); err != nil {
						log.Printf("Failed to export sub-page: %v", err)
					}
				}
			}
		}
	}

	return nil
}

func (e *Exporter) getExportFilename(page notion.Page) string {
	filename := filepath.Join(e.Directory, transformer.SimpleID(page.ID)+".md")
	// TODO slug the title?
	if e.UseTitleAsFilename {
		if title, err := transformer.GetPageTitle(page); err == nil {
			if len(e.ReplaceTitle) == 2 {
				title = strings.ReplaceAll(title, e.ReplaceTitle[0], e.ReplaceTitle[1])
			}
			title = strings.TrimSpace(title)

			filename = filepath.Join(e.Directory, title+".md")
		}
	}
	return filename
}

func (e *Exporter) StartDownloader(wg *sync.WaitGroup, size int) chan *transformer.AssetFuture {
	taskPool := make(chan *transformer.AssetFuture, size)

	for i := 0; i < size; i++ {
		wg.Add(1)

		go func() {
			for asset := range taskPool {
				filename, err := e.downloadAsset(asset)
				asset.Write(filename, err)

				if err != nil {
					log.Printf("Failed to download: %v", err)
				}
			}

			wg.Done()
		}()
	}

	return taskPool
}

var imgExtension = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp)$`)

func (e *Exporter) downloadAsset(asset *transformer.AssetFuture) (string, error) {
	if e.AssetDirectory == "" {
		return "", fmt.Errorf("config assetDirectory is empty")
	}

	if !imgExtension.MatchString(asset.Extension) {
		return "", fmt.Errorf("unsupported extension: %v", asset.Extension)
	}

	filename := e.getAssetFilename(asset)
	// skip if the filename already exists, assume downloaded before
	if _, err := os.Stat(filename); err == nil {
		return filename, nil
	}

	file, err := os.Create(filename)
	if err != nil {
		return filename, fmt.Errorf("create file, name: %v, err: %v", filename, err)
	}
	defer file.Close()

	resp, err := http.Get(asset.URL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("statusCode: %v, URL: %v", resp.StatusCode, asset.URL)
	}

	if _, err := io.Copy(file, resp.Body); err != nil {
		return filename, fmt.Errorf("write file, URL: %v, err: %v", asset.URL, err)
	}

	return filename, nil
}

func (e *Exporter) getAssetFilename(asset *transformer.AssetFuture) string {
	return filepath.Join(e.AssetDirectory, transformer.SimpleID(asset.BlockID)+asset.Extension)
}

func (e *Exporter) findPageByIDWithRetry(ctx context.Context, pageID string) (notion.Page, error) {
	var page notion.Page
	err := retry.Do(func() error {
		var innerErr error
		page, innerErr = e.Client.FindPageByID(ctx, pageID)
		return innerErr
	})
	return page, err
}

func (e *Exporter) findBlockChildrenByIDWithRetry(ctx context.Context, blockID string, query *notion.PaginationQuery) (notion.BlockChildrenResponse, error) {
	var resp notion.BlockChildrenResponse
	err := retry.Do(func() error {
		var innerErr error
		resp, innerErr = e.Client.FindBlockChildrenByID(ctx, blockID, query)
		return innerErr
	})
	return resp, err
}
