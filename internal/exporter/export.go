package exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/notionops"
	"github.com/zhuochun/notion-toolset/notionread"
	"github.com/zhuochun/notion-toolset/transformer"
)

type Exporter struct {
	Logger      *log.Logger
	AssetClient *http.Client
	DebugMode   bool
	ExecOne     string

	Client *notion.Client
	config.ExporterConfig

	notionReader *notionread.Reader

	downloadPool chan *transformer.AssetFuture

	exportedFilesMu sync.Mutex
	exportedFiles   map[string]struct{}

	slugger transformer.SlugRegistry
}

func (e *Exporter) Validate() error {

	if e.ExecOne != "" {
		if e.Directory == "" {
			e.Directory, _ = os.Getwd()
		}

		if reflect.DeepEqual(e.Markdown, transformer.MarkdownConfig{}) {
			e.Markdown = transformer.MarkdownConfig{
				NoAlias:        true,
				NoFrontMatters: true,
				NoMetadata:     true,
				TitleToH1:      true,
				PlainText:      true,
			}
		}
	}

	if err := config.CheckDirectory(e.Directory); err != nil {
		return err
	}

	if e.AssetDirectory != "" {
		if err := config.CheckDirectory(e.AssetDirectory); err != nil {
			return err
		}
	}

	if e.ExportSpeed < 1 {
		e.ExportSpeed = 2.8
	} else if e.ExportSpeed > 3 {
		e.ExportSpeed = 3
	}

	return nil
}

func (e *Exporter) Run() error {
	e.notionReader = notionops.NewReader(e.Client, e.ExportSpeed)
	e.exportedFiles = map[string]struct{}{}

	exportWg := new(sync.WaitGroup)
	exportPool := e.startExporter(exportWg, int(e.ExportSpeed))

	downloadWg := new(sync.WaitGroup)
	downloads := assetDownloader{directory: e.AssetDirectory, client: e.AssetClient, logger: e.logger()}
	e.downloadPool = downloads.start(downloadWg, int(e.ExportSpeed)*2)

	pageNum := 0
	scanErr := e.ScanPages(context.Background(), func(page notion.Page) error {
		pageNum++
		exportPool <- page
		if e.DebugMode && pageNum%500 == 0 {
			e.logger().Printf("Scanned pages: %v so far", pageNum)
		}
		return nil
	})
	e.logger().Printf("Scanned pages: %v", pageNum)

	close(exportPool)
	exportWg.Wait()

	close(e.downloadPool)
	downloadWg.Wait()

	if scanErr != nil {
		return scanErr
	}

	if err := e.cleanupDeletedPages(); err != nil {
		return err
	}

	return nil
}

func (e *Exporter) ScanPages(ctx context.Context, visit func(notion.Page) error) error {
	if e.ExecOne != "" {
		page, err := e.reader().Page(ctx, e.ExecOne)
		if err != nil {
			return err
		}
		return visit(page)
	}

	return e.scanDatabasePages(ctx, visit)
}

func (e *Exporter) scanDatabasePages(ctx context.Context, visit func(notion.Page) error) error {
	q := notionops.NewDatabaseQuery(e.Client, e.DatabaseID)

	date := ""
	if e.LookbackDays > 0 {
		date = time.Now().AddDate(0, 0, -e.LookbackDays).Format(notionops.DateLayout)
	}

	if err := q.SetQuery(e.DatabaseQuery, notionops.QueryBuilder{Date: date}); err != nil {
		e.logger().Panicf("Invalid query: %v, err: %v", e.DatabaseQuery, err)
	}

	if e.DebugMode {
		e.logger().Printf("DatabaseQuery Filter: %+v", q.Query.Filter)
		e.logger().Printf("DatabaseQuery Sorter: %+v", q.Query.Sorts)
	}

	return q.ForEach(ctx, e.DebugLimit, e.reader(), visit)
}

func (e *Exporter) writeDebugCache(id string, v interface{}) {
	filename := "temp/" + id + ".json"
	file, err := os.Create(filename)
	if err != nil {
		e.logger().Printf("Failed to create debug cache, name: %v, err: %v", filename, err)
		return
	}

	c, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		e.logger().Printf("Failed to marshal debug cache, name: %v, err: %v", filename, err)
		return
	}

	file.Write(c)
	file.Close()
}

func (e *Exporter) startExporter(wg *sync.WaitGroup, size int) chan notion.Page {
	taskPool := make(chan notion.Page, size)

	for i := 0; i < size; i++ {
		wg.Add(1)

		go func() {
			for page := range taskPool {
				if err := e.exportPage(page); err != nil {
					e.logger().Printf("Failed to export: %v", err)
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

	snapshot, err := e.reader().BlockSnapshot(context.TODO(), page.ID, notionread.BestEffort)
	if err != nil {
		return fmt.Errorf("query block id: %v, err: %v", page.ID, err)
	}
	blocks := snapshot.Roots()
	if e.DebugCache {
		for _, blockID := range snapshot.LoadedBlockIDs() {
			loaded, childErr := snapshot.Children(blockID)
			if childErr == nil {
				e.writeDebugCache(blockID, loaded)
			}
		}
	}

	filename := e.getExportFilename(page)
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("create file: %v, err: %v", filename, err)
	}
	defer file.Close()

	if e.DebugMode {
		e.logger().Printf("Exported to file: [%v] -> %v", page.ID, filename)
	}

	renderMarkdown(file, e.Markdown, &page, snapshot, e.downloadPool)
	e.trackExportedFile(filename)

	for _, block := range blocks {
		switch b := block.(type) {
		case *notion.ChildPageBlock:
			if child, err := e.reader().Page(context.Background(), b.ID()); err == nil {
				if err := e.exportPage(child); err != nil {
					e.logger().Printf("Failed to export sub-page: %v", err)
				}
			}
		case *notion.LinkToPageBlock:
			if b.PageID != "" {
				if child, err := e.reader().Page(context.Background(), b.PageID); err == nil {
					if err := e.exportPage(child); err != nil {
						e.logger().Printf("Failed to export sub-page: %v", err)
					}
				}
			}
		}
	}

	return nil
}

func (e *Exporter) reader() *notionread.Reader {
	if e.notionReader != nil {
		return e.notionReader
	}
	return notionread.New(e.Client, notionread.WithConcurrency(max(1, int(e.ExportSpeed))))
}

func (e *Exporter) logger() *log.Logger {
	if e.Logger != nil {
		return e.Logger
	}
	return log.Default()
}
