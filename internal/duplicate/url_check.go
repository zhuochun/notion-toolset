package duplicate

import (
	"context"
	"net/http"
	"time"

	"github.com/dstotijn/go-notion"
)

var duplicateURLHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
}

// brokenURLCheck checks if the configured URL property of the page is broken.
// It returns true when the URL is set but leads to a non-OK HTTP status or
// errors during request. When BrokenURLProperty is empty, it always returns
// false.
func (d *DuplicateChecker) brokenURLCheck(page notion.Page) bool {
	if d.BrokenURLProperty == "" {
		return false
	}

	props, ok := page.Properties.(notion.DatabasePageProperties)
	if !ok {
		return false
	}

	prop, ok := props[d.BrokenURLProperty]
	if !ok {
		return false
	}

	urlStr := stringifyDBProp(prop)
	if urlStr == "" {
		return false
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodHead, urlStr, nil)
	if err != nil {
		return true
	}
	resp, err := duplicateURLHTTPClient.Do(req)
	if err != nil || resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		getReq, getErr := http.NewRequestWithContext(context.Background(), http.MethodGet, urlStr, nil)
		if getErr != nil {
			return true
		}
		getResp, getErr := duplicateURLHTTPClient.Do(getReq)
		if getErr != nil {
			return true
		}
		defer getResp.Body.Close()
		return getResp.StatusCode >= http.StatusBadRequest
	}
	if err != nil {
		return true
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return true
	}
	return false
}
