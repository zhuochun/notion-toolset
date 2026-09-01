package main

import (
	"errors"
	"net"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/retry"
)

func retryNotion(fn func() error) error {
	return retry.DoIf(fn, isRetryableNotionError)
}

func isRetryableNotionError(err error) bool {
	if errors.Is(err, notion.ErrRateLimited) ||
		errors.Is(err, notion.ErrConflict) ||
		errors.Is(err, notion.ErrInternalServer) ||
		errors.Is(err, notion.ErrServiceUnavailable) {
		return true
	}

	var apiErr *notion.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == 429 || apiErr.Status >= 500
	}

	var netErr net.Error
	return errors.As(err, &netErr)
}
