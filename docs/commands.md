# Commands

Run `notion-toolset --cmd=<name> --config=<file.yaml>`. Source users can replace
`notion-toolset` with `go run .`. See [setup](../README.md),
[configuration](configuration.md), and [canonical examples](../example/configs/).

## Invocation

| Flag | Default | Meaning |
| --- | --- | --- |
| `--cmd` | empty | `daily-journal`, `weekly-journal`, `flashback`, `duplicate`, `collector`, `export`, `upload`, or `llm` |
| `--config` | empty | YAML file, read relative to the working directory |
| `--one` | empty | Direct page ID for export/LLM; a leading `https:` URL is searched for a 32-character ID |
| `--multi` | false | Decode a YAML list of configurations |
| `--idx` | -1 | In multi mode, nonnegative selects one zero-based index; negative uses all |
| `--repeat` | 1 | Repeat the selected configurations in order, with no delay; zero/negative performs no command runs |
| `--debug` | false | Additional diagnostics, including decoded configuration |
| `--mode` | `dry-run` | Upload mode: `dry-run`, `discard`, `upload`, or `resolve` |
| `--workspace` | `https://www.notion.so` | Notion link base for upload resolve UI |
| `--port` | 17889 | Resolve UI port; upload normalizes nonpositive values to 17889 |
| `--help` / `-h` | — | Flag help on stderr; succeeds without credentials |

Flags use Go's `flag` syntax; parsing stops at the first positional argument.
There are no positional subcommands. Flags unrelated to the selected command
remain accepted. `--one` allows an omitted config file at loading time; only
export and LLM actually consume that page ID.

`NOTION_TOKEN` is required before configuration loading or execution, even for
upload dry-run or `--repeat=0`. LLM credentials are checked only when LLM is
selected and validated. An explicit config path is always read. Multi mode always
reads its file, even with `--one`. An empty list with a negative index succeeds
without running anything; index 0 into an empty list fails. Repetition stops at
the first returned validation/run error.

Normal exit codes: 0 for help/success, 1 for missing token, invalid selection or
workflow error, and 2 for flag/config read/decode errors. Existing panic paths
(for example some malformed query templates) still terminate the process; they
are not converted into ordinary validation errors. Logs normally go to stderr;
transformer alias diagnostics can write to stdout. A successful exit can include
the partial failures described below.

## Notion request limits

All CLI Notion reads and writes share a paced HTTP client (three requests per
second, burst one). HTTP 429 and 529 rejections are retried up to five total
attempts, preserving the request body. Each retry waits at least the server's
`Retry-After` value (seconds or HTTP date) and the exponential fallback of
1, 2, 4, then 8 seconds. Missing or invalid headers use the fallback. Cancellation
interrupts waiting. Retry exhaustion returns an error to the existing workflow
failure policy; the read layer does not start another batch of those retries.

A valid `Retry-After` pauses subsequent requests from the same client, including
other workers. Generic server errors and network failures do not cause automatic
write replay. Existing read retries remain available for transient read errors.
This budget is local to each client: scheduled jobs sharing a Notion token should
run sequentially to avoid competing for the same server limit. Export/LLM speed
settings can impose a slower read limit. External asset, URL-check, and LLM API
requests are separate from the Notion client.

## Workflow behavior

| Command / YAML section | Inputs and effects | Failure behavior |
| --- | --- | --- |
| `daily-journal` / `dailyJournal` | Query existing titles once; create up to `limit` future daily entries starting tomorrow in local time | Query/title errors stop; each create failure is logged and later dates continue |
| `weekly-journal` / `weeklyJournal` | Same policy for Monday–Sunday entries; Monday itself advances to the following Monday | Same continuation as daily journals |
| `flashback` / `flashback` | Resolve today's journal when configured; randomly sample a historical query window, retry at the oldest window if empty, append chosen references, optionally write IDs to a chain file | Target/query errors stop. Individual reference-write failures do not fail Run; chain contains selected IDs regardless of those writes. Map iteration order is unspecified |
| `duplicate` / `duplicateChecker` | Scan pages; report duplicate keys and optionally broken URLs to the dump block. Configured properties use OR semantics; omitted properties use title. Ignore empty/unsupported values | A page is reported at most once per run. A report failure stops scanning; prior reports remain. Broken URLs may send HEAD then GET to the property URL |
| `collector` / `collector` | Read collection blocks recursively, completely scan candidates, exclude collected page IDs, append new references in batches of 100 | Discovery or scan failure prevents all writes. Batch build/write failures are logged/count as failed, later batches continue, and Run succeeds |
| `export` / `exporter` | Scan database or one page; traverse page content, render Markdown, download configured assets, optionally clean stale files | Scan errors return after queued work completes and suppress cleanup. Page-worker and asset errors are logged; worker failures do not suppress cleanup after a successful scan |
| `upload` / `exporter` | Discover changed files in a Git worktree; compare local/base/Notion, then perform selected mode | Per-file failures continue and produce a final error. Replacement is not atomic; see below |
| `llm` / `llm` | Read direct page, chain IDs, or database; render plain text; filter by configured byte-length bounds; invoke LLM and append results | Per-page worker failures are logged; Run returns the scan error. Group mode propagates completion/write errors. A missing chain file is logged and treated as no input |

Journal queries use one API result page, as before. A limit counts future dates
considered, not successful creations; skipped existing entries do not extend it.
Flashback requires an oldest timestamp sufficiently in the past to make the
random hour bound positive; invalid bounds retain the existing panic behavior.

LLM group mode joins eligible page contents with newlines. Its target is today's
configured journal, otherwise the direct page when available, otherwise the last
scanned page. A configured but missing journal is an error. Text results become
nonempty paragraphs (leading `- ` is removed); JSON results are interpreted with
`respTextBlock`. This is not a transactional write or a guaranteed lossless
round trip of arbitrary model responses.

## Export files and cleanup

`--one` defaults to the working directory and plain text with an H1 title only
when the entire Markdown config is zero. Database export needs `directory`.
Directories are created if absent. Paths are relative to the process working
directory, including asset and alias paths; config file location does not rebase them.

By default filenames use the compact page ID. Title filenames use the existing
Windows-safe slug/collision policy and optional two-string replacement. Markdown
properties/aliases follow the configuration reference. Notion-hosted assets use
block-ID filenames; external images remain external links. Asset failures retain
the remote URL in output and may leave an incomplete local file. Existing asset
files are reused without refreshing them.

Cleanup runs only when `cleanupDeleted: true`, the scan succeeds, `--one` is empty,
and `lookbackDays` is not positive. It removes regular, non-hidden `.md` files
directly in the export directory that were not tracked as exported. It does not
recurse or remove assets. A debug limit or custom query still narrows the export
set used by full cleanup. Use a dedicated output directory and inspect logs:
cleanup is not a proof that a page was deleted remotely.

## Upload modes and recovery limits

Run inside a Git repository. `directory` defaults to the working directory and
must be inside that repository. Candidate discovery includes staged, unstaged,
and untracked paths under that directory. Unreadable files and files with no
32-character page ID in their filename/frontmatter are skipped. Comparison reads
current Notion content and may download assets even in dry-run mode. It does not
perform export cleanup. BOM/CRLF/CR differences are ignored for comparison.

| Mode | Behavior |
| --- | --- |
| `dry-run` | Report match and merge state; no Notion writes or Git discard |
| `discard` | Discard only files matching current Notion content. Tracked paths are restored in both index and worktree from HEAD; untracked paths are passed to `git clean -f`. Other paths stay unchanged |
| `upload` | Skip matches; allow mismatches only with `mergeable` or `no-base` status. A clean merge check permits replacement with the local content; it does not upload a synthesized merge |
| `resolve` | Serve embedded UI on `127.0.0.1:<port>` until interrupted; list files, compare on demand, and allow explicit Git discard from the UI. This UI does not submit Notion replacements |

Resolve serves `/`, `/api/files`, `/api/diff?id=N`, and
`/api/discard?id=N` (POST). Its comparison state is kept in memory for that run;
restart to rediscover changed files. The HTML is included in the binary and needs
no runtime file beside it. There is no new background service or shutdown protocol.

Replacement strips export frontmatter and, when `titleToH1` is true, removes the
leading H1 and updates the Notion database title. The supported converter includes
paragraphs, headings, bulleted/numbered items, tasks, quotes, dividers and fenced
code. It is a small line-oriented converter; nested structure, rich inline styles,
tables, media and arbitrary Notion blocks are not a lossless import format.

After optional title update, upload enumerates **fresh, paginated top-level
children** using the same reader as comparison, deletes them in order, and appends
replacement blocks in batches of 100. Title failure prevents child reads; read
failure prevents all deletes/appends; delete failure stops later deletes/appends.
An append failure can leave the page partly or entirely cleared. Earlier title,
delete, or append effects remain. Only explicit rate-limit/overload rejections
receive automatic write retries as described above; there is no rollback.
Inspect Notion and retained local/Git content before deciding what to retry; Git
discard changes local state and is not a remote recovery action.
