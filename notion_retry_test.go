package main

import (
	"fmt"
	"testing"

	"github.com/dstotijn/go-notion"
)

func TestIsRetryableNotionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "rate limited", err: fmt.Errorf("query: %w", notion.ErrRateLimited), want: true},
		{name: "conflict", err: notion.ErrConflict, want: true},
		{name: "internal server", err: notion.ErrInternalServer, want: true},
		{name: "service unavailable", err: notion.ErrServiceUnavailable, want: true},
		{name: "unknown server error", err: &notion.APIError{Status: 502}, want: true},
		{name: "validation", err: notion.ErrValidation, want: false},
		{name: "object not found", err: notion.ErrObjectNotFound, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableNotionError(tt.err); got != tt.want {
				t.Fatalf("expected retryable=%v, got %v", tt.want, got)
			}
		})
	}
}
