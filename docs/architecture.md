# Current code ownership

One Go module and one root executable, with internal packages for workflow
ownership. The direct-switch decision is recorded in the
[restructure spec](specs/202609-repo-restructure.md). Update this map and focused
checks in [DEVELOPMENT.md](../DEVELOPMENT.md) when changing an owner.

## Application and effects

| Responsibility | Current owner | Important boundary |
| --- | --- | --- |
| Process exit | `main.go` | Thin canonical executable; root build/install path retained |
| Flags/help/diagnostics | `internal/cli` | Fresh flag set and stderr logger per invocation; returns exit classification |
| Environment, clients, command construction, repeat/multi | `internal/app` | Composes concrete workflows and lazy clients; no workflow policy |
| YAML types/decoding and directory prerequisites | `internal/config` | Permissive runtime decoding; defaults/validation remain with selected workflow |
| Daily and weekly journal creation | `internal/journal` | Local time and query/property templates; individual create failures log and continue |
| Flashback, duplicate detection, collection | `internal/flashback`, `internal/duplicate`, `internal/collector` | Own selection/reporting and Notion writes; collector completes discovery and full scan before any writes |
| Query and write-template construction | `internal/notionops` | Shared query/template/append mechanisms and tuned reader construction; no workflow decisions |
| Notion reads | `notionread/`, especially `doc.go` | Owns retry classification, rate limiting, pagination, and structural snapshots. Callers choose strict or best-effort completeness and page-graph traversal policy. |
| Retry mechanism | `retry/` | Owns backoff and cancellation during waits; callers supply retry policy. |
| Markdown rendering and supporting filename/alias logic | `transformer/` | Consumes snapshots; asset futures connect rendering to downloads. Rendering is not the Notion read owner. |
| Export orchestration, files, assets, cleanup | `internal/exporter` | Owns bulk workers/streaming files and shared concrete RenderSession; worker errors log rather than fail Run |
| Reverse upload, Git state, comparison, resolve UI | `internal/upload` | Owns modes, fresh pre-delete reads and replacement effects; HTML embedded at build time |
| LLM workflows | `internal/llm` | Filtering, grouping, request composition and output; app supplies lazy client setup |
| Local HTTP fixtures | `internal/notiontest` | Test consumers only; redirects SDK requests to local servers |
| Shared full gate | `scripts/verify` | Fixed test/vet/build scope for local and CI use |

## Representative paths

**Config/dispatch:** root -> CLI -> app -> config/workflow. App checks the token,
decodes config, selects configurations and constructs a new workflow for each
repeat. Validate retains default/setup timing. Clock/random function fields have
ordinary production defaults. Root tests execute the built binary outside the
repository; CLI/app/config tests cover isolation, ordering, selection and examples.

**Export:** `Exporter.Run` -> database scan through `DatabaseQuery`/`notionread`
-> page workers -> best-effort block snapshot -> `transformer` -> local Markdown
and asset downloads -> wait for all work -> optional cleanup after successful
scan. Tests distinguish scan failures from logged worker failures and preserve
helper contracts. Public notionread tests retain pagination, completeness,
cancellation and concurrency evidence.

**Upload:** discover candidates -> invoke app-supplied reader factory once -> lend
that same reader to `exporter.RenderSession` -> compare -> selected mode.
Replacement optionally updates title, freshly paginates children through the
shared reader, deletes in order, then appends. It never reuses the comparison
snapshot or resets the read budget. Completed effects survive a later failure.
Tests use temporary Git repositories and local HTTP services for mode, handler,
pagination and failure-order evidence.

## Resource and dependency boundaries

RenderSession owns asset workers and synchronous rendering, serialized with Close
by one mutex. Failed reads leave it reusable; Close waits/drains, is idempotent,
and rejects subsequent rendering before I/O. It does not close its borrowed
reader. Normal upload closes on return; resolve retains it for the existing
process/server lifetime. Bulk export uses the same private rendering helper
without serializing its page-worker pool or buffering entire pages.

Both render sessions and bulk export use the private `assetDownloader` in
`assets.go`, which holds only download dependencies. Sessions do not construct
an Exporter workflow to obtain download workers. Export and LLM page queues are
local to Run; their worker start methods are private.

LLM page and group execution share `pageContent` for snapshot rendering and
byte-length eligibility. Their callers retain separate error policies: per-page
workers log and continue, while group execution returns the error.

Workflows use config/notionops/notionread/transformer; none imports app or CLI.
Upload additionally uses exporter; exporter never imports upload. Public packages
never import workflows. Config references the existing MarkdownConfig instead of
copying it. There are no compatibility facades or secondary executables. Upload's
Git, parser and HTTP concerns remain files in one package.

## Change routes and limits

- Config: change input representation and the selected workflow's policy, retain
  canonical example tests, and update [configuration.md](configuration.md).
- Command: compose a concrete workflow in app's existing switch, add behavior and
  invocation tests, and update [commands.md](commands.md).
- Cleanup: exporter owns managed paths/tracking/timing; cover full/incremental/
  direct runs and scan versus worker errors.
- Upload recovery: start with commands.md, then upload's compare/upload/git files
  and effect tests. Git discard is not rollback of a Notion replacement.
- Rendering: transformer owns bytes/aliases/slugs, exporter owns assets/lifetime,
  notionread owns completeness.

The [verification record](specs/202609-repo-restructure-verification.md) maps the
accepted claims to evidence. Local tests and the full gate do not establish live
service success, exhaustive Markdown round trips, atomic writes or race freedom.
Existing partial failures and panic paths are characterized, not repaired here.
