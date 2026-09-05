// Package transformer renders Notion snapshots as Markdown and owns aliases,
// slugs, and asset futures. Existing public imports and APIs remain supported.
// Rendering can read an alias directory and synchronously wait on asset futures;
// it is not a pure formatter. Callers supply snapshots and own download workers.
// Best-effort snapshots retain available content and log missing children.
package transformer
