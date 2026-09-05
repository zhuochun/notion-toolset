package notiontest

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/dstotijn/go-notion"
)

func Client(t *testing.T, baseURL string) *notion.Client {
	t.Helper()

	target, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}

	httpClient := &http.Client{
		Transport: rewriteHostTransport{target: target},
	}

	return notion.NewClient("token", notion.WithHTTPClient(httpClient))
}

type rewriteHostTransport struct {
	target *url.URL
}

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.target.Scheme
	clone.URL.Host = t.target.Host
	clone.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(clone)
}
