// Package notionread owns read-only interaction with Notion: retry and
// backoff, rate limiting, pagination, and structural block snapshots.
//
// It deliberately does not own query-template compilation, page-graph policy,
// content transformation, asset downloads, or Notion writes.
package notionread
