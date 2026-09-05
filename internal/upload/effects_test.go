package upload

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/notiontest"
	"github.com/zhuochun/notion-toolset/notionread"
	"github.com/zhuochun/notion-toolset/transformer"
)

func TestComparisonThenFreshReplacementFailureOrder(t *testing.T) {
	for _, failure := range []string{"", "title", "read", "delete", "append"} {
		t.Run("failure="+failure, func(t *testing.T) {
			var events []string
			reads := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fail := func() {
					w.WriteHeader(400)
					io.WriteString(w, `{"object":"error","status":400,"code":"validation_error","message":"fixture failure"}`)
				}
				switch {
				case r.Method == "GET" && strings.Contains(r.URL.Path, "/pages/"):
					events = append(events, "compare-page")
					io.WriteString(w, `{"object":"page","id":"page","parent":{"type":"database_id","database_id":"db"},"properties":{"Name":{"type":"title","title":[]}}}`)
				case r.Method == "PATCH" && strings.Contains(r.URL.Path, "/pages/"):
					events = append(events, "title")
					if failure == "title" {
						fail()
						return
					}
					io.WriteString(w, `{"object":"page","id":"page","parent":{"type":"database_id","database_id":"db"},"properties":{}}`)
				case r.Method == "GET":
					reads++
					event := "compare-children"
					id := "old"
					more := false
					if reads > 1 {
						event = "fresh-1"
						id = "new-1"
						more = true
					}
					if reads > 2 {
						event = "fresh-2"
						id = "new-2"
						more = false
					}
					events = append(events, event)
					if failure == "read" && reads == 3 {
						fail()
						return
					}
					fmt.Fprintf(w, `{"object":"list","results":[{"object":"block","id":%q,"type":"paragraph","paragraph":{"rich_text":[]}}],"has_more":%v,"next_cursor":"next"}`, id, more)
				case r.Method == "DELETE":
					events = append(events, "delete-"+filepath.Base(r.URL.Path))
					if failure == "delete" {
						fail()
						return
					}
					fmt.Fprintf(w, `{"object":"block","id":%q,"type":"paragraph","paragraph":{"rich_text":[]}}`, filepath.Base(r.URL.Path))
				case r.Method == "PATCH":
					events = append(events, "append")
					body, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(body), "replacement") {
						t.Errorf("append payload: %s", body)
					}
					if failure == "append" {
						fail()
						return
					}
					io.WriteString(w, `{"object":"list","results":[]}`)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
			}))
			defer s.Close()
			client := notiontest.Client(t, s.URL)
			factoryCalls := 0
			r := ReverseUploader{Client: client, Logger: log.New(io.Discard, "", 0), NewReader: func(float64) *notionread.Reader { factoryCalls++; return notionread.New(client) }, ExporterConfig: config.ExporterConfig{ExportSpeed: 3, Markdown: transformer.MarkdownConfig{NoAlias: true, NoFrontMatters: true, NoMetadata: true, TitleToH1: true}}}
			if err := r.startRenderer(); err != nil {
				t.Fatal(err)
			}
			defer r.session.Close()
			_, page, err := r.exportPageMarkdown("page")
			if err != nil {
				t.Fatal(err)
			}
			err = r.uploadPage(page, []byte("# New title\n\nreplacement\n"))
			if (err != nil) != (failure != "") {
				t.Fatalf("upload=%v", err)
			}
			want := []string{"compare-page", "compare-children", "title"}
			if failure != "title" {
				want = append(want, "fresh-1", "fresh-2")
			}
			if failure != "title" && failure != "read" {
				want = append(want, "delete-new-1")
				if failure != "delete" {
					want = append(want, "delete-new-2", "append")
				}
			}
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("events=%v want=%v", events, want)
			}
			if factoryCalls != 1 {
				t.Fatalf("reader budget reset: %d constructions", factoryCalls)
			}
		})
	}
}

func gitFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("Git is required for upload regression tests: %v", err)
	}
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.name", "Fixture")
	runGit(t, root, "config", "user.email", "fixture@example.invalid")
	runGit(t, root, "config", "core.autocrlf", "false")
	return root
}
func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, err := gitOutput(root, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func writeFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestGitCandidatesAndScopedDiscard(t *testing.T) {
	root := gitFixture(t)
	dir := filepath.Join(root, "notes")
	for _, name := range []string{"staged space.md", "未暂存.md"} {
		writeFixture(t, filepath.Join(dir, name), "base\n")
	}
	writeFixture(t, filepath.Join(root, "outside.md"), "base\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "fixture")
	writeFixture(t, filepath.Join(dir, "staged space.md"), "staged\n")
	runGit(t, root, "add", "--", "notes/staged space.md")
	writeFixture(t, filepath.Join(dir, "未暂存.md"), "unstaged\n")
	writeFixture(t, filepath.Join(dir, "new 文.md"), "new\n")
	writeFixture(t, filepath.Join(root, "outside.md"), "outside changed\n")
	r := ReverseUploader{repoRoot: root, ExporterConfig: config.ExporterConfig{Directory: dir}}
	files, err := r.changedFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("candidates=%v", files)
	}
	for _, file := range files {
		if err := r.discard(r.gitRelPath(file)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"staged space.md", "未暂存.md"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(b) != "base\n" {
			t.Fatalf("restored %s=%q err=%v", name, b, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "new 文.md")); !os.IsNotExist(err) {
		t.Fatalf("untracked not removed: %v", err)
	}
	status := runGit(t, root, "status", "--porcelain")
	if strings.TrimSpace(status) != "M outside.md" {
		t.Fatalf("unrelated change or index disturbed: %q", status)
	}
	if got := r.mergeState("notes/未暂存.md", []byte("local\n"), []byte("base\r\n")); got != "mergeable" {
		t.Fatalf("merge state=%s", got)
	}
	if got := r.mergeState("missing.md", []byte("local"), []byte("remote")); got != "no-base" {
		t.Fatalf("missing base=%s", got)
	}
}

func TestResolveHTTPContracts(t *testing.T) {
	root := gitFixture(t)
	path := filepath.Join(root, "new.md")
	writeFixture(t, path, "new\n")
	r := ReverseUploader{repoRoot: root, Workspace: "https://example.invalid/<script>", resolveFiles: []reverseResolveFile{{ID: 0, Path: path, RelPath: "new.md", Compared: true, Match: true, Diff: "fixture diff", Merge: "match"}}}
	for _, tc := range []struct {
		method, url string
		handler     http.HandlerFunc
		code        int
		text        string
	}{
		{"GET", "/", r.handleResolveIndex, 200, "<!doctype html>"},
		{"GET", "/api/files", r.handleResolveFiles, 200, `"relPath":"new.md"`},
		{"GET", "/api/diff?id=0", r.handleResolveDiff, 200, "fixture diff"},
		{"GET", "/api/diff?id=9", r.handleResolveDiff, 404, "file not found"},
		{"GET", "/api/diff?id=no", r.handleResolveDiff, 400, "invalid"},
		{"GET", "/api/discard?id=0", r.handleResolveDiscard, 405, "method not allowed"},
		{"POST", "/api/discard?id=0", r.handleResolveDiscard, 200, "discarded"},
	} {
		w := httptest.NewRecorder()
		tc.handler(w, httptest.NewRequest(tc.method, tc.url, nil))
		if w.Code != tc.code || !strings.Contains(strings.ToLower(w.Body.String()), strings.ToLower(tc.text)) {
			t.Fatalf("%s %s: %d %s", tc.method, tc.url, w.Code, w.Body.String())
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("discard did not remove file: %v", err)
	}
}
