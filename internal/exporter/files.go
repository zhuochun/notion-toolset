package exporter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/transformer"
)

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
		e.logger().Printf("Deleted stale export: %v", filename)
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
