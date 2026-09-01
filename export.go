package main

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/zhuochun/notion-toolset/notionread"
	"github.com/zhuochun/notion-toolset/transformer"
	"golang.org/x/time/rate"
)

var exportAssetHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
}

type ExporterConfig struct {
	DatabaseID    string `yaml:"databaseID"`
	DatabaseQuery string `yaml:"databaseQuery"`
	// export related
	LookbackDays       int      `yaml:"lookbackDays"`   // leave this empty for full backup
	Directory          string   `yaml:"directory"`      // output directory
	AssetDirectory     string   `yaml:"assetDirectory"` // output directory for assets (images, etc)
	CleanupDeleted     bool     `yaml:"cleanupDeleted"`
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
	notionReader *notionread.Reader

	exportPool   chan notion.Page
	downloadPool chan *transformer.AssetFuture

	exportedFilesMu sync.Mutex
	exportedFiles   map[string]struct{}

	slugger transformer.SlugRegistry
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
	e.notionReader = e.newReader()
	e.exportedFiles = map[string]struct{}{}

	// workers to write markdowns
	exportWg := new(sync.WaitGroup)
	e.exportPool = e.StartExporter(exportWg, int(e.ExportSpeed))
	// workers to download assets
	downloadWg := new(sync.WaitGroup)
	e.downloadPool = e.StartDownloader(downloadWg, int(e.ExportSpeed)*2)

	// query database pages, queue each pages for export
	pageNum := 0
	scanErr := e.ScanPages(context.Background(), func(page notion.Page) error {
		pageNum++
		e.exportPool <- page
		if e.DebugMode && pageNum%500 == 0 {
			log.Printf("Scanned pages: %v so far", pageNum)
		}
		return nil
	})
	log.Printf("Scanned pages: %v", pageNum)

	close(e.exportPool)
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
	q := NewDatabaseQuery(e.Client, e.DatabaseID)

	date := "" // default
	if e.LookbackDays > 0 {
		date = time.Now().AddDate(0, 0, -e.LookbackDays).Format(layoutDate)
	}

	if err := q.SetQuery(e.DatabaseQuery, QueryBuilder{Date: date}); err != nil {
		log.Panicf("Invalid query: %v, err: %v", e.DatabaseQuery, err)
	}

	if e.DebugMode {
		log.Printf("DatabaseQuery Filter: %+v", q.Query.Filter)
		log.Printf("DatabaseQuery Sorter: %+v", q.Query.Sorts)
	}

	return q.ForEach(ctx, e.DebugLimit, e.queryLimiter, visit)
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
		log.Printf("Exported to file: [%v] -> %v", page.ID, filename)
	}

	t := transformer.New(e.Markdown, &page, snapshot, e.downloadPool)
	t.TransformOut(file)
	e.trackExportedFile(filename)

	// export sub-pages inside this page
	for _, block := range blocks {
		switch b := block.(type) {
		case *notion.ChildPageBlock:
			if child, err := e.reader().Page(context.Background(), b.ID()); err == nil {
				if err := e.exportPage(child); err != nil {
					log.Printf("Failed to export sub-page: %v", err)
				}
			}
		case *notion.LinkToPageBlock:
			if b.PageID != "" {
				if child, err := e.reader().Page(context.Background(), b.PageID); err == nil {
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
	slug := transformer.SimpleID(page.ID)

	if e.UseTitleAsFilename {
		if title, err := transformer.GetPageTitle(page); err == nil {
			if len(e.ReplaceTitle) == 2 {
				title = strings.ReplaceAll(title, e.ReplaceTitle[0], e.ReplaceTitle[1])
			}
			title = strings.TrimSpace(title)

			if cleaned := transformer.SlugifyTitle(title, transformer.MaxSlugLength); cleaned != "" {
				slug = cleaned
			}

			slug = e.slugger.Register(slug, page.ID)
		} else {
			slug = e.slugger.Register(slug, page.ID)
		}
	}

	return filepath.Join(e.Directory, slug+".md")
}

func (e *Exporter) trackExportedFile(filename string) {
	e.exportedFilesMu.Lock()
	defer e.exportedFilesMu.Unlock()

	if e.exportedFiles == nil {
		e.exportedFiles = map[string]struct{}{}
	}
	e.exportedFiles[filename] = struct{}{}
}

func (e *Exporter) cleanupDeletedPages() error {
	if !e.CleanupDeleted || e.ExecOne != "" || e.LookbackDays > 0 {
		return nil
	}

	entries, err := os.ReadDir(e.Directory)
	if err != nil {
		return fmt.Errorf("read export directory: %v, err: %w", e.Directory, err)
	}

	exportedFiles := e.snapshotExportedFiles()
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !isManagedMarkdownFile(entry.Name()) {
			continue
		}

		filename := filepath.Join(e.Directory, entry.Name())
		if _, ok := exportedFiles[filename]; ok {
			continue
		}

		if err := e.removeManagedFile(filename, e.Directory); err != nil {
			return err
		}
	}

	return nil
}

func (e *Exporter) snapshotExportedFiles() map[string]struct{} {
	e.exportedFilesMu.Lock()
	defer e.exportedFilesMu.Unlock()

	snapshot := make(map[string]struct{}, len(e.exportedFiles))
	for filename := range e.exportedFiles {
		snapshot[filename] = struct{}{}
	}
	return snapshot
}

func isManagedMarkdownFile(name string) bool {
	return filepath.Ext(name) == ".md" && !strings.HasPrefix(name, ".")
}

func (e *Exporter) removeManagedFile(filename, rootDir string) error {
	if filename == "" {
		return nil
	}

	managed, err := isManagedPath(rootDir, filename)
	if err != nil {
		return fmt.Errorf("validate managed path: %v, err: %w", filename, err)
	}
	if !managed {
		return fmt.Errorf("refuse to delete file outside export directory: %v", filename)
	}
	if err := os.Remove(filename); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete exported file: %v, err: %w", filename, err)
	}

	if e.DebugMode {
		log.Printf("Deleted stale export: %v", filename)
	}
	return nil
}

func isManagedPath(rootDir, filename string) (bool, error) {
	rootAbs, err := filepath.Abs(rootDir)
	if err != nil {
		return false, err
	}
	fileAbs, err := filepath.Abs(filename)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(rootAbs, fileAbs)
	if err != nil {
		return false, err
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)), nil
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

var assetExtension = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp|mp4|mov|webm|mkv|avi|mp3|wav|m4a|flac|ogg|pdf)$`)

func (e *Exporter) downloadAsset(asset *transformer.AssetFuture) (string, error) {
	if e.AssetDirectory == "" {
		return "", fmt.Errorf("config assetDirectory is empty")
	}

	if !assetExtension.MatchString(asset.Extension) {
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

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", err
	}

	resp, err := exportAssetHTTPClient.Do(req)
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

func (e *Exporter) reader() *notionread.Reader {
	if e.notionReader != nil {
		return e.notionReader
	}
	return e.newReader()
}

func (e *Exporter) newReader() *notionread.Reader {
	return notionread.New(e.Client,
		notionread.WithLimiter(e.queryLimiter),
		notionread.WithConcurrency(max(1, int(e.ExportSpeed))),
	)
}
