package upload

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/exporter"
	"github.com/zhuochun/notion-toolset/internal/notionops"
	"github.com/zhuochun/notion-toolset/notionread"
)

const (
	reverseModeDryRun  = "dry-run"
	reverseModeDiscard = "discard"
	reverseModeUpload  = "upload"
	reverseModeResolve = "resolve"
)

type ReverseUploader struct {
	Logger      *log.Logger
	DebugMode   bool
	Mode        string
	Workspace   string
	ResolvePort int

	Client *notion.Client
	config.ExporterConfig

	repoRoot  string
	NewReader func(float64) *notionread.Reader
	reader    *notionread.Reader
	session   *exporter.RenderSession

	resolveMu    sync.Mutex
	resolveFiles []reverseResolveFile
}

func (r *ReverseUploader) Validate() error {
	if r.Mode == "" {
		r.Mode = reverseModeDryRun
	}
	switch r.Mode {
	case reverseModeDryRun, reverseModeDiscard, reverseModeUpload, reverseModeResolve:
	default:
		return fmt.Errorf("invalid mode %q, use dry-run, discard, upload, or resolve", r.Mode)
	}
	r.Workspace = normalizeWorkspaceURL(r.Workspace)
	if r.ResolvePort <= 0 {
		r.ResolvePort = 17889
	}

	root, err := gitOutput("", "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("upload can only run inside a git repo: %w", err)
	}
	r.repoRoot = strings.TrimSpace(root)

	if r.Directory == "" {
		r.Directory, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	r.Directory, err = filepath.Abs(r.Directory)
	if err != nil {
		return err
	}
	if err := ensureInsideRepo(r.repoRoot, r.Directory); err != nil {
		return err
	}
	if err := config.CheckDirectory(r.Directory); err != nil {
		return err
	}
	if r.AssetDirectory != "" {
		r.AssetDirectory, err = filepath.Abs(r.AssetDirectory)
		if err != nil {
			return err
		}
		if err := config.CheckDirectory(r.AssetDirectory); err != nil {
			return err
		}
	}
	if r.ExportSpeed < 1 {
		r.ExportSpeed = 2.8
	} else if r.ExportSpeed > 3 {
		r.ExportSpeed = 3
	}
	return nil
}

func (r *ReverseUploader) Run() error {
	changed, err := r.changedFiles()
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		r.logger().Printf("No uncommitted files in %v", r.Directory)
		return nil
	}

	if err := r.startRenderer(); err != nil {
		return err
	}
	defer r.session.Close()

	if r.Mode == reverseModeResolve {
		return r.runResolve(changed)
	}

	var failed bool
	for _, filename := range changed {
		status, err := r.processFile(filename)
		if err != nil {
			failed = true
			status = reverseFileStatus{
				Path:       filename,
				RelPath:    r.gitRelPath(filename),
				MergeState: "error",
				Message:    err.Error(),
			}
		}
		r.logStatus(status)
	}

	if failed {
		return errors.New("upload completed with errors")
	}
	return nil
}

func (r *ReverseUploader) startRenderer() error {
	if r.NewReader == nil {
		return errors.New("upload requires a reader factory")
	}
	r.reader = r.NewReader(r.ExportSpeed)
	session, err := exporter.NewRenderSession(r.reader, r.ExporterConfig, nil, r.logger())
	if err != nil {
		return err
	}
	r.session = session
	return nil
}

func (r *ReverseUploader) uploadPage(page notion.Page, local []byte) error {
	title, body := r.uploadBody(local)
	if title != "" {
		if err := r.updateTitle(page, title); err != nil {
			return err
		}
	}

	blocks := markdownToNotionBlocks(body)
	children, err := r.reader.BlockChildren(context.Background(), page.ID)
	if err != nil {
		return err
	}
	for _, child := range children {
		if _, err := r.Client.DeleteBlock(context.Background(), child.ID()); err != nil {
			return err
		}
	}

	appendBlock := notionops.NewAppendBlock(r.Client, page.ID)
	appendBlock.Blocks = blocks
	_, err = appendBlock.Do(context.Background())
	return err
}

func (r *ReverseUploader) updateTitle(page notion.Page, title string) error {
	props, ok := page.Properties.(notion.DatabasePageProperties)
	if !ok {
		return nil
	}
	for key, prop := range props {
		if prop.Type != notion.DBPropTypeTitle {
			continue
		}
		props := notion.DatabasePageProperties{
			key: {
				Title: richTextChunks(title),
			},
		}
		_, err := r.Client.UpdatePage(context.Background(), page.ID, notion.UpdatePageParams{
			DatabasePageProperties: props,
		})
		return err
	}
	return nil
}

func (r *ReverseUploader) logger() *log.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return log.Default()
}
