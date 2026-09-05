package upload

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/dstotijn/go-notion"
)

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

func (r *ReverseUploader) exportPageMarkdown(pageID string) (string, notion.Page, error) {
	page, content, err := r.session.RenderPage(context.Background(), pageID)
	return string(content), page, err
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

func (r *ReverseUploader) logStatus(status reverseFileStatus) {
	if status.Skipped {
		r.logger().Printf("%s: %s", status.RelPath, status.Message)
		return
	}

	match := "mismatch"
	if status.Match {
		match = "match"
	}
	if status.Message != "" {
		r.logger().Printf("%s: %s, diff=%s, %s", status.RelPath, match, status.MergeState, status.Message)
		return
	}
	r.logger().Printf("%s: %s, diff=%s", status.RelPath, match, status.MergeState)
}

func equalIgnoringEOL(a, b []byte) bool {
	return normalizeEOL(string(a)) == normalizeEOL(string(b))
}

func normalizeEOL(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}
