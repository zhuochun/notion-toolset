package upload

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

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

func (r *ReverseUploader) gitRelPath(filename string) string {
	rel, err := filepath.Rel(r.repoRoot, filename)
	if err != nil {
		return filename
	}
	return filepath.ToSlash(rel)
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
