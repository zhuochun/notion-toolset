package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/notionread"
	"github.com/zhuochun/notion-toolset/transformer"
)

const (
	reverseModeDryRun  = "dry-run"
	reverseModeDiscard = "discard"
	reverseModeUpload  = "upload"
	reverseModeResolve = "resolve"
)

type ReverseUploader struct {
	DebugMode   bool
	Mode        string
	Workspace   string
	ResolvePort int

	Client *notion.Client
	ExporterConfig

	repoRoot   string
	exporter   *Exporter
	downloadWg *sync.WaitGroup

	resolveMu    sync.Mutex
	resolveFiles []reverseResolveFile
}

type reverseFileStatus struct {
	Path       string
	RelPath    string
	PageID     string
	Match      bool
	MergeState string
	Message    string
	Skipped    bool
}

type reverseFileCompare struct {
	Status reverseFileStatus
	Local  []byte
	Remote []byte
	Page   notion.Page
}

type reverseResolveFile struct {
	ID        int    `json:"id"`
	Path      string `json:"-"`
	RelPath   string `json:"relPath"`
	PageID    string `json:"pageID"`
	NotionURL string `json:"notionURL"`
	Compared  bool   `json:"compared"`
	Match     bool   `json:"match"`
	Merge     string `json:"merge"`
	Skipped   bool   `json:"skipped"`
	Message   string `json:"message"`
	Discarded bool   `json:"discarded"`

	Diff string `json:"-"`
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
	if err := (&Exporter{ExporterConfig: r.ExporterConfig}).precheckDir(r.Directory); err != nil {
		return err
	}
	if r.AssetDirectory != "" {
		r.AssetDirectory, err = filepath.Abs(r.AssetDirectory)
		if err != nil {
			return err
		}
		if err := (&Exporter{ExporterConfig: r.ExporterConfig}).precheckDir(r.AssetDirectory); err != nil {
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
		log.Printf("No uncommitted files in %v", r.Directory)
		return nil
	}

	r.startExporter()
	defer r.stopExporter()

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

func (r *ReverseUploader) startExporter() {
	r.exporter = &Exporter{
		DebugMode:      r.DebugMode,
		Client:         r.Client,
		ExporterConfig: r.ExporterConfig,
	}
	r.exporter.notionReader = newNotionReader(r.Client, r.ExportSpeed)

	r.downloadWg = new(sync.WaitGroup)
	r.exporter.downloadPool = r.exporter.StartDownloader(r.downloadWg, int(r.ExportSpeed)*2)
}

func (r *ReverseUploader) stopExporter() {
	if r.exporter != nil && r.exporter.downloadPool != nil {
		close(r.exporter.downloadPool)
		r.downloadWg.Wait()
	}
}

func (r *ReverseUploader) changedFiles() ([]string, error) {
	dirRel, err := filepath.Rel(r.repoRoot, r.Directory)
	if err != nil {
		return nil, err
	}
	dirRel = filepath.ToSlash(dirRel)
	if dirRel == "." {
		dirRel = ""
	}

	paths := map[string]struct{}{}
	commands := [][]string{
		gitPathspecArgs([]string{"diff", "--name-only", "-z"}, dirRel),
		gitPathspecArgs([]string{"diff", "--cached", "--name-only", "-z"}, dirRel),
		gitPathspecArgs([]string{"ls-files", "--others", "--exclude-standard", "-z"}, dirRel),
	}
	for _, args := range commands {
		out, err := gitOutputBytes(r.repoRoot, args...)
		if err != nil {
			return nil, err
		}
		for _, rel := range splitNUL(out) {
			if rel == "" {
				continue
			}
			paths[rel] = struct{}{}
		}
	}

	files := make([]string, 0, len(paths))
	for rel := range paths {
		files = append(files, filepath.Join(r.repoRoot, filepath.FromSlash(rel)))
	}
	sort.Strings(files)
	return files, nil
}

func (r *ReverseUploader) processFile(filename string) (reverseFileStatus, error) {
	compare, err := r.compareFile(filename)
	status := compare.Status
	if err != nil {
		return status, err
	}
	if status.Skipped {
		return status, nil
	}

	switch r.Mode {
	case reverseModeDryRun:
		return status, nil
	case reverseModeDiscard:
		if status.Match {
			if err := r.discard(status.RelPath); err != nil {
				return status, err
			}
			status.Message = "discarded"
		}
	case reverseModeUpload:
		if status.Match {
			status.Message = "skip: already matches Notion"
			return status, nil
		}
		if status.MergeState != "mergeable" && status.MergeState != "no-base" {
			return status, fmt.Errorf("refuse upload when diff=%s (allowed: mergeable, no-base)", status.MergeState)
		}
		if err := r.uploadPage(compare.Page, compare.Local); err != nil {
			return status, err
		}
		status.Message = "uploaded"
	}
	return status, nil
}

func (r *ReverseUploader) compareFile(filename string) (reverseFileCompare, error) {
	status := reverseFileStatus{
		Path:    filename,
		RelPath: r.gitRelPath(filename),
	}

	local, err := os.ReadFile(filename)
	if err != nil {
		status.Skipped = true
		status.Message = "skip: local file is not readable"
		return reverseFileCompare{Status: status}, nil
	}

	pageID := extractNotionPageID(filename, local)
	if pageID == "" {
		status.Skipped = true
		status.Message = "skip: no Notion page id in filename or frontmatter"
		return reverseFileCompare{Status: status}, nil
	}
	status.PageID = pageID

	remote, page, err := r.exportPageMarkdown(pageID)
	if err != nil {
		return reverseFileCompare{Status: status}, err
	}

	remoteBytes := []byte(remote)
	status.Match = equalIgnoringEOL(local, remoteBytes)
	status.MergeState = r.mergeState(status.RelPath, local, remoteBytes)

	return reverseFileCompare{
		Status: status,
		Local:  local,
		Remote: remoteBytes,
		Page:   page,
	}, nil
}

func (r *ReverseUploader) runResolve(changed []string) error {
	r.resolveMu.Lock()
	r.resolveFiles = nil
	r.resolveMu.Unlock()

	for _, filename := range changed {
		r.appendResolveFile(r.scanResolveFile(filename))
	}

	addr, err := r.startResolveServer()
	if err != nil {
		return err
	}
	log.Printf("Resolve UI: http://%s", addr)
	log.Printf("Press Ctrl+C to stop.")
	select {}
}

func (r *ReverseUploader) scanResolveFile(filename string) reverseResolveFile {
	row := reverseResolveFile{
		Path:    filename,
		RelPath: r.gitRelPath(filename),
		Message: "not compared yet",
	}

	local, err := os.ReadFile(filename)
	if err != nil {
		row.Skipped = true
		row.Message = "skip: local file is not readable"
		return row
	}
	pageID := extractNotionPageID(filename, local)
	if pageID == "" {
		row.Skipped = true
		row.Message = "skip: no Notion page id in filename or frontmatter"
		return row
	}
	row.PageID = pageID
	row.NotionURL = notionPageURL(r.Workspace, pageID)
	return row
}

func (r *ReverseUploader) appendResolveFile(row reverseResolveFile) {
	r.resolveMu.Lock()
	defer r.resolveMu.Unlock()
	row.ID = len(r.resolveFiles)
	r.resolveFiles = append(r.resolveFiles, row)
}

func (r *ReverseUploader) exportPageMarkdown(pageID string) (string, notion.Page, error) {
	page, err := r.exporter.reader().Page(context.Background(), pageID)
	if err != nil {
		return "", notion.Page{}, err
	}
	snapshot, err := r.exporter.reader().BlockSnapshot(context.Background(), page.ID, notionread.BestEffort)
	if err != nil {
		return "", notion.Page{}, err
	}

	var out bytes.Buffer
	t := transformer.New(r.Markdown, &page, snapshot, r.exporter.downloadPool)
	t.TransformOut(&out)
	return out.String(), page, nil
}

func (r *ReverseUploader) mergeState(relPath string, local, remote []byte) string {
	base, ok := r.headFile(relPath)
	if !ok {
		if equalIgnoringEOL(local, remote) {
			return "match"
		}
		return "no-base"
	}
	if equalIgnoringEOL(local, remote) {
		return "match"
	}
	if clean, err := gitMergeClean(local, base, remote); err == nil {
		if clean {
			return "mergeable"
		}
		return "conflict"
	}
	return "unknown"
}

func (r *ReverseUploader) headFile(relPath string) ([]byte, bool) {
	out, err := gitOutputBytes(r.repoRoot, "show", "HEAD:"+filepath.ToSlash(relPath))
	return out, err == nil
}

func (r *ReverseUploader) discard(relPath string) error {
	if _, ok := r.headFile(relPath); ok {
		_, err := gitOutputBytes(r.repoRoot, "restore", "--staged", "--worktree", "--", filepath.ToSlash(relPath))
		return err
	}
	_, err := gitOutputBytes(r.repoRoot, "clean", "-f", "--", filepath.ToSlash(relPath))
	return err
}

func (r *ReverseUploader) uploadPage(page notion.Page, local []byte) error {
	title, body := r.uploadBody(local)
	if title != "" {
		if err := r.updateTitle(page, title); err != nil {
			return err
		}
	}

	blocks := markdownToNotionBlocks(body)
	children, err := r.exporter.reader().BlockChildren(context.Background(), page.ID)
	if err != nil {
		return err
	}
	for _, child := range children {
		if _, err := r.Client.DeleteBlock(context.Background(), child.ID()); err != nil {
			return err
		}
	}

	appendBlock := NewAppendBlock(r.Client, page.ID)
	appendBlock.Blocks = blocks
	_, err = appendBlock.Do(context.Background())
	return err
}

func (r *ReverseUploader) uploadBody(local []byte) (string, string) {
	body := stripFrontMatter(string(local))
	if r.Markdown.TitleToH1 {
		return stripLeadingH1(body)
	}
	return "", body
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

func (r *ReverseUploader) logStatus(status reverseFileStatus) {
	if status.Skipped {
		log.Printf("%s: %s", status.RelPath, status.Message)
		return
	}

	match := "mismatch"
	if status.Match {
		match = "match"
	}
	if status.Message != "" {
		log.Printf("%s: %s, diff=%s, %s", status.RelPath, match, status.MergeState, status.Message)
		return
	}
	log.Printf("%s: %s, diff=%s", status.RelPath, match, status.MergeState)
}

func (r *ReverseUploader) startResolveServer() (string, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", r.ResolvePort)
	listener, err := netListen(addr)
	if err != nil {
		return "", fmt.Errorf("resolve UI listen failed on %s: %w", addr, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", r.handleResolveIndex)
	mux.HandleFunc("/api/files", r.handleResolveFiles)
	mux.HandleFunc("/api/diff", r.handleResolveDiff)
	mux.HandleFunc("/api/discard", r.handleResolveDiscard)

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("resolve server error: %v", err)
		}
	}()

	return listener.Addr().String(), nil
}

func (r *ReverseUploader) handleResolveIndex(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = resolveIndexTemplate.Execute(w, map[string]string{
		"Workspace": r.Workspace,
	})
}

func (r *ReverseUploader) handleResolveFiles(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	respondJSON(w, map[string]interface{}{
		"files": r.resolveSnapshot(),
	})
}

func (r *ReverseUploader) handleResolveDiff(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := parseResolveID(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	diff, ok := r.resolveCompare(id)
	if !ok {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(diff))
}

func (r *ReverseUploader) handleResolveDiscard(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := parseResolveID(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	relPath, ok, invalid := r.resolveDiscardTarget(id)
	if !ok {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if invalid {
		http.Error(w, "discard is not available for this file", http.StatusBadRequest)
		return
	}

	if err := r.discard(relPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	r.removeResolveFile(id)
	respondJSON(w, map[string]string{"status": "discarded"})
}

func (r *ReverseUploader) resolveSnapshot() []reverseResolveFile {
	r.resolveMu.Lock()
	defer r.resolveMu.Unlock()
	files := make([]reverseResolveFile, len(r.resolveFiles))
	for i, row := range r.resolveFiles {
		copyRow := row
		copyRow.Diff = ""
		files[i] = copyRow
	}
	return files
}

func (r *ReverseUploader) resolveCompare(id int) (string, bool) {
	r.resolveMu.Lock()
	if id < 0 || id >= len(r.resolveFiles) {
		r.resolveMu.Unlock()
		return "", false
	}
	row := r.resolveFiles[id]
	if row.Compared {
		diff := row.Diff
		r.resolveMu.Unlock()
		return diff, true
	}
	r.resolveMu.Unlock()

	compare, err := r.compareFile(row.Path)
	if err != nil {
		msg := "compare error: " + err.Error()
		r.resolveMu.Lock()
		if id >= 0 && id < len(r.resolveFiles) {
			r.resolveFiles[id].Compared = true
			r.resolveFiles[id].Merge = "error"
			r.resolveFiles[id].Message = msg
			r.resolveFiles[id].Diff = msg
		}
		r.resolveMu.Unlock()
		return msg, true
	}
	status := compare.Status
	if status.Skipped {
		r.resolveMu.Lock()
		if id >= 0 && id < len(r.resolveFiles) {
			r.resolveFiles[id].Compared = true
			r.resolveFiles[id].Skipped = true
			r.resolveFiles[id].Message = status.Message
			r.resolveFiles[id].Diff = status.Message
		}
		r.resolveMu.Unlock()
		return status.Message, true
	}

	diff, err := unifiedDiff(compare.Remote, compare.Local, "notion/"+status.RelPath, "local/"+status.RelPath)
	if err != nil {
		msg := "diff error: " + err.Error()
		r.resolveMu.Lock()
		if id >= 0 && id < len(r.resolveFiles) {
			r.resolveFiles[id].Compared = true
			r.resolveFiles[id].PageID = status.PageID
			r.resolveFiles[id].NotionURL = notionPageURL(r.Workspace, status.PageID)
			r.resolveFiles[id].Match = status.Match
			r.resolveFiles[id].Merge = status.MergeState
			r.resolveFiles[id].Message = msg
			r.resolveFiles[id].Diff = msg
		}
		r.resolveMu.Unlock()
		return msg, true
	}

	message := status.Message
	if message == "" {
		message = "compared"
	}
	r.resolveMu.Lock()
	if id >= 0 && id < len(r.resolveFiles) {
		r.resolveFiles[id].Compared = true
		r.resolveFiles[id].PageID = status.PageID
		r.resolveFiles[id].NotionURL = notionPageURL(r.Workspace, status.PageID)
		r.resolveFiles[id].Match = status.Match
		r.resolveFiles[id].Merge = status.MergeState
		r.resolveFiles[id].Message = message
		r.resolveFiles[id].Diff = diff
	}
	r.resolveMu.Unlock()
	return diff, true
}

func (r *ReverseUploader) resolveDiscardTarget(id int) (string, bool, bool) {
	r.resolveMu.Lock()
	defer r.resolveMu.Unlock()
	if id < 0 || id >= len(r.resolveFiles) {
		return "", false, false
	}
	row := r.resolveFiles[id]
	if row.RelPath == "" {
		return "", true, true
	}
	return row.RelPath, true, false
}

func (r *ReverseUploader) removeResolveFile(id int) {
	r.resolveMu.Lock()
	defer r.resolveMu.Unlock()
	if id < 0 || id >= len(r.resolveFiles) {
		return
	}
	r.resolveFiles = append(r.resolveFiles[:id], r.resolveFiles[id+1:]...)
	for i := range r.resolveFiles {
		r.resolveFiles[i].ID = i
	}
}

func (r *ReverseUploader) gitRelPath(filename string) string {
	rel, err := filepath.Rel(r.repoRoot, filename)
	if err != nil {
		return filename
	}
	return filepath.ToSlash(rel)
}

func extractNotionPageID(filename string, content []byte) string {
	if match := hashIDRegex.FindString(filepath.Base(filename)); match != "" {
		return match
	}
	frontMatter := frontMatter(content)
	if frontMatter == "" {
		return ""
	}
	return hashIDRegex.FindString(frontMatter)
}

func frontMatter(content []byte) string {
	s := normalizeEOL(string(content))
	if !strings.HasPrefix(s, "---\n") {
		return ""
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end == -1 {
		return ""
	}
	return rest[:end]
}

func stripFrontMatter(content string) string {
	s := normalizeEOL(content)
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end == -1 {
		return s
	}
	after := rest[end+len("\n---"):]
	return strings.TrimLeft(after, "\n")
}

func stripLeadingH1(content string) (string, string) {
	lines := strings.Split(normalizeEOL(content), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			title := strings.TrimSpace(strings.TrimPrefix(line, "# "))
			body := strings.Join(lines[i+1:], "\n")
			return title, strings.TrimLeft(body, "\n")
		}
		return "", content
	}
	return "", content
}

func markdownToNotionBlocks(content string) []notion.Block {
	lines := strings.Split(normalizeEOL(content), "\n")
	var blocks []notion.Block
	var paragraph []string
	var code []string
	var codeLang string
	inCode := false

	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		text := strings.Join(paragraph, "\n")
		blocks = append(blocks, &notion.ParagraphBlock{RichText: richTextChunks(text)})
		paragraph = nil
	}
	flushCode := func() {
		text := strings.Join(code, "\n")
		lang := codeLang
		if lang == "" {
			lang = "plain text"
		}
		blocks = append(blocks, &notion.CodeBlock{RichText: richTextChunks(text), Language: &lang})
		code = nil
		codeLang = ""
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				flushCode()
				inCode = false
			} else {
				flushParagraph()
				inCode = true
				codeLang = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			}
			continue
		}
		if inCode {
			code = append(code, line)
			continue
		}
		if trimmed == "" {
			flushParagraph()
			continue
		}
		if block, ok := markdownLineBlock(line); ok {
			flushParagraph()
			blocks = append(blocks, block)
			continue
		}
		paragraph = append(paragraph, line)
	}
	if inCode {
		flushCode()
	}
	flushParagraph()
	return blocks
}

func markdownLineBlock(line string) (notion.Block, bool) {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "### "):
		return &notion.Heading3Block{RichText: richTextChunks(strings.TrimSpace(strings.TrimPrefix(trimmed, "### ")))}, true
	case strings.HasPrefix(trimmed, "## "):
		return &notion.Heading2Block{RichText: richTextChunks(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))}, true
	case strings.HasPrefix(trimmed, "# "):
		return &notion.Heading1Block{RichText: richTextChunks(strings.TrimSpace(strings.TrimPrefix(trimmed, "# ")))}, true
	case strings.HasPrefix(trimmed, "- [ ] "):
		checked := false
		return &notion.ToDoBlock{RichText: richTextChunks(strings.TrimSpace(strings.TrimPrefix(trimmed, "- [ ] "))), Checked: &checked}, true
	case strings.HasPrefix(strings.ToLower(trimmed), "- [x] "):
		checked := true
		return &notion.ToDoBlock{RichText: richTextChunks(strings.TrimSpace(trimmed[6:])), Checked: &checked}, true
	case strings.HasPrefix(trimmed, "- "):
		return &notion.BulletedListItemBlock{RichText: richTextChunks(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))}, true
	case numberedListRegex.MatchString(trimmed):
		return &notion.NumberedListItemBlock{RichText: richTextChunks(numberedListRegex.ReplaceAllString(trimmed, ""))}, true
	case strings.HasPrefix(trimmed, "> "):
		return &notion.QuoteBlock{RichText: richTextChunks(strings.TrimSpace(strings.TrimPrefix(trimmed, "> ")))}, true
	}
	return nil, false
}

var numberedListRegex = regexp.MustCompile(`^\d+\.\s+`)

func richTextChunks(text string) []notion.RichText {
	if text == "" {
		return nil
	}
	const maxRichTextLen = 1900
	var chunks []notion.RichText
	runes := []rune(text)
	for len(runes) > 0 {
		n := maxRichTextLen
		if len(runes) < n {
			n = len(runes)
		}
		chunk := string(runes[:n])
		runes = runes[n:]
		chunks = append(chunks, notion.RichText{
			Type:      notion.RichTextTypeText,
			PlainText: chunk,
			Text:      &notion.Text{Content: chunk},
		})
	}
	return chunks
}

func equalIgnoringEOL(a, b []byte) bool {
	return normalizeEOL(string(a)) == normalizeEOL(string(b))
}

func normalizeEOL(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func ensureInsideRepo(repoRoot, path string) error {
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("directory must be inside git repo: %v", path)
	}
	return nil
}

func gitMergeClean(local, base, remote []byte) (bool, error) {
	tmpDir, err := os.MkdirTemp("", "notion-toolset-merge-*")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(tmpDir)

	localPath := filepath.Join(tmpDir, "local.md")
	basePath := filepath.Join(tmpDir, "base.md")
	remotePath := filepath.Join(tmpDir, "remote.md")
	for path, content := range map[string][]byte{
		localPath:  []byte(normalizeEOL(string(local))),
		basePath:   []byte(normalizeEOL(string(base))),
		remotePath: []byte(normalizeEOL(string(remote))),
	} {
		if err := os.WriteFile(path, content, 0644); err != nil {
			return false, err
		}
	}

	cmd := exec.Command("git", "merge-file", "--stdout", localPath, basePath, remotePath)
	err = cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

func gitOutput(dir string, args ...string) (string, error) {
	out, err := gitOutputBytes(dir, args...)
	return string(out), err
}

func gitOutputBytes(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return out, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return out, err
	}
	return out, nil
}

func gitPathspecArgs(base []string, pathspec string) []string {
	args := append([]string{}, base...)
	args = append(args, "--")
	if pathspec != "" {
		args = append(args, pathspec)
	}
	return args
}

func splitNUL(out []byte) []string {
	raw := strings.Split(string(out), "\x00")
	var items []string
	for _, item := range raw {
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}

func parseResolveID(req *http.Request) (int, error) {
	idRaw := req.URL.Query().Get("id")
	if idRaw == "" {
		return 0, fmt.Errorf("missing id")
	}
	id, err := strconv.Atoi(idRaw)
	if err != nil {
		return 0, fmt.Errorf("invalid id")
	}
	return id, nil
}

func respondJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func netListen(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

func normalizeWorkspaceURL(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	workspace = strings.TrimRight(workspace, "/")
	if workspace == "" {
		return "https://www.notion.so"
	}
	if strings.Contains(workspace, "://") {
		return workspace
	}
	workspace = strings.TrimLeft(workspace, "/")
	if strings.Contains(workspace, ".") && !strings.Contains(workspace, "/") {
		return "https://" + workspace
	}
	return "https://www.notion.so/" + workspace
}

func notionPageURL(workspace, pageID string) string {
	if pageID == "" {
		return ""
	}
	return strings.TrimRight(workspace, "/") + "/" + pageID
}

func unifiedDiff(remote, local []byte, remoteLabel, localLabel string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "notion-toolset-diff-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	remotePath := filepath.Join(tmpDir, "remote.md")
	localPath := filepath.Join(tmpDir, "local.md")
	if err := os.WriteFile(remotePath, []byte(normalizeEOL(string(remote))), 0644); err != nil {
		return "", err
	}
	if err := os.WriteFile(localPath, []byte(normalizeEOL(string(local))), 0644); err != nil {
		return "", err
	}

	cmd := exec.Command("git", "diff", "--no-index", "--unified=3", "--", remotePath, localPath)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return "", err
		}
	}
	diff := string(out)
	diff = strings.ReplaceAll(diff, "a/"+filepath.ToSlash(remotePath), "a/"+remoteLabel)
	diff = strings.ReplaceAll(diff, "b/"+filepath.ToSlash(localPath), "b/"+localLabel)
	if strings.TrimSpace(diff) == "" {
		return "No differences", nil
	}
	return diff, nil
}

var resolveIndexTemplate = template.Must(template.New("resolve-index").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8"/>
  <title>notion-toolset upload resolve</title>
  <style>
    body { margin: 0; font-family: Arial, sans-serif; background: #f7f7f8; color: #222; }
    .layout { display: flex; height: 100vh; }
    .left { width: 420px; min-width: 420px; max-width: 420px; flex: 0 0 420px; border-right: 1px solid #ddd; background: #fff; display: flex; flex-direction: column; }
    .right { flex: 1; display: flex; flex-direction: column; }
    .header { padding: 10px 12px; border-bottom: 1px solid #eee; font-size: 13px; background: #fafafa; }
    .list { overflow: auto; padding: 0; margin: 0; list-style: none; }
    .item { border-bottom: 1px solid #f1f1f1; padding: 8px 10px; cursor: pointer; }
    .item.active { background: #eef6ff; }
    .item .pathRow { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
    .item .path { font-size: 13px; word-break: break-all; }
    .item .status { flex: 0 0 auto; font-size: 11px; color: #6b7280; min-width: 62px; text-align: right; }
    .item .status.loading { color: #0b63d1; }
    .item .status.loaded { color: #18794e; font-weight: 600; }
    .item .meta { margin-top: 4px; color: #666; font-size: 12px; }
    .actions { position: relative; padding: 10px 12px; border-bottom: 1px solid #ddd; background: #fff; display: flex; gap: 8px; align-items: center; }
    .actions a, .actions button { font-size: 12px; padding: 6px 10px; border-radius: 4px; border: 1px solid #bbb; background: #fff; color: #111; text-decoration: none; cursor: pointer; }
    .actions #openLink { border-color: #1f7a3f; background: #1f7a3f; color: #fff; }
    .actions #openLink:hover { background: #176033; }
    .actions button:disabled, .actions a.disabled { opacity: .45; pointer-events: none; }
    .confirm-pop { position: absolute; display: none; min-width: 230px; background: #fff; border: 1px solid #d1d5db; border-radius: 6px; box-shadow: 0 10px 24px rgba(0,0,0,0.16); padding: 8px; z-index: 5; }
    .confirm-pop.show { display: block; }
    .confirm-pop .title { font-size: 12px; margin-bottom: 8px; color: #222; }
    .confirm-pop .row { display: flex; justify-content: flex-end; gap: 6px; }
    .confirm-pop .row button { font-size: 12px; padding: 5px 9px; border-radius: 4px; border: 1px solid #bbb; background: #fff; cursor: pointer; }
    .confirm-pop .row .danger { border-color: #b42318; background: #b42318; color: #fff; }
    .confirm-pop .row .danger:hover { background: #921b13; }
    .diff { margin: 0; padding: 12px; overflow: auto; background: #0f111a; color: #e6e6e6; font-family: Consolas, monospace; font-size: 12px; line-height: 1.4; white-space: pre; flex: 1; }
    .diff .hint { color: #8b91a1; }
    .diff .line { display: block; white-space: pre; }
    .diff .line.meta { color: #9aa3b2; }
    .diff .line.hunk { color: #6cb6ff; }
    .diff .line.add { color: #b4f0c6; background: rgba(34, 197, 94, 0.15); }
    .diff .line.del { color: #fecaca; background: rgba(239, 68, 68, 0.16); }
    .badge { display: inline-block; border: 1px solid #ddd; border-radius: 4px; padding: 0 6px; margin-right: 6px; font-size: 11px; color: #444; }
  </style>
</head>
<body>
  <div class="layout">
    <div class="left">
      <div class="header">Files (uncommitted under exporter.directory)</div>
      <ul id="fileList" class="list"></ul>
    </div>
    <div class="right">
      <div class="actions">
        <span id="state"></span>
        <a id="openLink" href="#" target="_blank" rel="noreferrer noopener" class="disabled">Open Notion</a>
        <button id="discardBtn" type="button">Discard Local</button>
        <div id="discardConfirm" class="confirm-pop">
          <div class="title">Discard local changes for this file?</div>
          <div class="row">
            <button id="discardCancelBtn" type="button">Cancel</button>
            <button id="discardConfirmBtn" type="button" class="danger">Discard</button>
          </div>
        </div>
      </div>
      <div id="diff" class="diff"><span class="hint">Select a file</span></div>
    </div>
  </div>
  <script>
    const workspace = {{ printf "%q" .Workspace }};
    let files = [];
    let selected = -1;
    let loadingId = -1;
    let diffReqId = 0;

    const fileList = document.getElementById("fileList");
    const diffBox = document.getElementById("diff");
    const stateBox = document.getElementById("state");
    const openLink = document.getElementById("openLink");
    const discardBtn = document.getElementById("discardBtn");
    const discardConfirm = document.getElementById("discardConfirm");
    const discardCancelBtn = document.getElementById("discardCancelBtn");
    const discardConfirmBtn = document.getElementById("discardConfirmBtn");

    function selectedFile() {
      return files.find(f => f.id === selected);
    }

    function rowMeta(file) {
      const tags = [];
      if (!file.compared) {
        tags.push("not-compared");
      } else {
        tags.push(file.match ? "match" : "mismatch");
        if (file.merge) tags.push("diff=" + file.merge);
      }
      if (file.skipped) tags.push("skipped");
      if (file.discarded) tags.push("discarded");
      return tags.join(" | ");
    }

    function rowStatus(file) {
      if (file.id === loadingId) return { text: "loading...", cls: "loading" };
      if (file.compared) return { text: "✓ loaded", cls: "loaded" };
      return { text: "", cls: "" };
    }

    function hideDiscardConfirm() {
      discardConfirm.classList.remove("show");
    }

    function showDiscardConfirm() {
      const btnRect = discardBtn.getBoundingClientRect();
      const containerRect = discardBtn.parentElement.getBoundingClientRect();
      const left = btnRect.left - containerRect.left;
      const top = btnRect.bottom - containerRect.top + 6;
      discardConfirm.style.left = left + "px";
      discardConfirm.style.top = top + "px";
      discardConfirm.classList.add("show");
    }

    function escapeHtml(value) {
      return value
        .replaceAll("&", "&amp;")
        .replaceAll("<", "&lt;")
        .replaceAll(">", "&gt;");
    }

    function renderDiffText(text) {
      if (!text) {
        diffBox.innerHTML = "";
        return;
      }
      const html = text.split("\n").map(line => {
        let cls = "line";
        if (line.startsWith("@@")) cls += " hunk";
        else if (line.startsWith("diff ") || line.startsWith("index ") || line.startsWith("--- ") || line.startsWith("+++ ")) cls += " meta";
        else if (line.startsWith("+") && !line.startsWith("+++")) cls += " add";
        else if (line.startsWith("-") && !line.startsWith("---")) cls += " del";
        return '<span class="' + cls + '">' + escapeHtml(line) + '</span>';
      }).join("");
      diffBox.innerHTML = html;
    }

    function renderList() {
      fileList.innerHTML = "";
      for (const file of files) {
        const li = document.createElement("li");
        li.className = "item" + (file.id === selected ? " active" : "");
        li.onclick = () => {
          selected = file.id;
          renderList();
          renderActions();
          loadDiff();
        };

        const pathRow = document.createElement("div");
        pathRow.className = "pathRow";

        const p = document.createElement("div");
        p.className = "path";
        p.textContent = file.relPath;
        pathRow.appendChild(p);

        const status = document.createElement("div");
        const statusView = rowStatus(file);
        status.className = "status" + (statusView.cls ? " " + statusView.cls : "");
        status.textContent = statusView.text;
        pathRow.appendChild(status);

        li.appendChild(pathRow);

        const m = document.createElement("div");
        m.className = "meta";
        m.textContent = rowMeta(file) + (file.message ? " | " + file.message : "");
        li.appendChild(m);
        fileList.appendChild(li);
      }
      if (files.length === 0) {
        const li = document.createElement("li");
        li.className = "item";
        li.textContent = "No files";
        fileList.appendChild(li);
      }
    }

    function renderActions() {
      const file = selectedFile();
      if (!file) {
        stateBox.textContent = "";
        openLink.href = "#";
        openLink.classList.add("disabled");
        discardBtn.disabled = true;
        hideDiscardConfirm();
        return;
      }
      stateBox.innerHTML = '<span class="badge">' + rowMeta(file) + '</span>';
      if (file.notionURL) {
        openLink.href = file.notionURL;
        openLink.classList.remove("disabled");
      } else {
        openLink.href = "#";
        openLink.classList.add("disabled");
      }
      discardBtn.disabled = file.discarded;
      if (discardBtn.disabled) {
        hideDiscardConfirm();
      }
    }

    async function loadFiles() {
      const res = await fetch("/api/files");
      const data = await res.json();
      files = data.files || [];
      if (!files.find(f => f.id === selected)) {
        selected = -1;
        loadingId = -1;
      }
      renderList();
      renderActions();
      if (selected < 0) {
        diffBox.innerHTML = '<span class="hint">Select a file</span>';
      }
    }

    async function loadDiff() {
      const file = selectedFile();
      if (!file) {
        diffBox.innerHTML = '<span class="hint">Select a file</span>';
        return;
      }
      const reqID = ++diffReqId;
      loadingId = file.id;
      renderList();
      renderActions();
      diffBox.innerHTML = "";

      const res = await fetch("/api/diff?id=" + encodeURIComponent(file.id));
      if (reqID !== diffReqId) return;

      let text = "";
      if (!res.ok) {
        text = await res.text();
      } else {
        text = await res.text();
      }
      loadingId = -1;
      await loadFiles();
      if (selected !== file.id) return;
      renderDiffText(text);
    }

    discardBtn.onclick = async () => {
      const file = selectedFile();
      if (!file || discardBtn.disabled) return;
      if (discardConfirm.classList.contains("show")) {
        hideDiscardConfirm();
      } else {
        showDiscardConfirm();
      }
    };

    discardCancelBtn.onclick = () => {
      hideDiscardConfirm();
    };

    discardConfirmBtn.onclick = async () => {
      const file = selectedFile();
      if (!file || discardBtn.disabled) return;
      hideDiscardConfirm();
      const res = await fetch("/api/discard?id=" + encodeURIComponent(file.id), { method: "POST" });
      if (!res.ok) {
        alert(await res.text());
      }
      await loadFiles();
    };

    document.addEventListener("click", (event) => {
      if (!discardConfirm.classList.contains("show")) return;
      if (discardConfirm.contains(event.target) || discardBtn.contains(event.target)) return;
      hideDiscardConfirm();
    });

    loadFiles();
  </script>
</body>
</html>`))
