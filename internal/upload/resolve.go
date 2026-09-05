package upload

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
)

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
	r.logger().Printf("Resolve UI: http://%s", addr)
	r.logger().Printf("Press Ctrl+C to stop.")
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
			r.logger().Printf("resolve server error: %v", err)
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

//go:embed resolve.html
var resolveIndexHTML string

var resolveIndexTemplate = template.Must(template.New("resolve-index").Parse(resolveIndexHTML))
