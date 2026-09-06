// Package notionread owns read-only interaction with Notion: retry and
// backoff, rate limiting, pagination, and structural block snapshots.
//
// It deliberately does not own query-template compilation, page-graph policy,
// content transformation, asset downloads, or Notion writes.
// Use a Notion client configured with notionhttp.NewTransport to preserve
// Retry-After headers and share HTTP pacing/cooldown across reads and writes.
// Bare SDK clients retain read retries but cannot expose response headers here.
package notionread
