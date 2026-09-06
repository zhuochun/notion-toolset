# Repository restructure verification

Scope: [202609-repo-restructure.md](202609-repo-restructure.md), direct switch from
`c252305fcac093db34923cc55145c94fd53c86a1`. Existing uncommitted agent setup was
preserved. No live Notion/LLM operations, commits, publication or release were run.

## Evidence by claim

| Claim | Executable evidence |
| --- | --- |
| CHG-01 | Root `TestBinaryCLI`: build root and invoke from a temporary directory; help, unknown/invalid flags, token/config precedence, unknown command and repeat bounds |
| CHG-02 | `internal/cli` invocation isolation; app multi/repeat/index and empty-list cases; original construction/execution-order tests retained |
| CHG-03 | Config runtime permissiveness/null/zero/error cases and all canonical examples; app local endpoint/credential test; notionops escaping test |
| CHG-04 | Journal local Monday/month/year boundaries and skip/create continuation; flashback fallback and chain; duplicate OR-property reporting and retained already-reported test |
| CHG-05 | Public transformer JSON/Markdown golden: nested content, properties, alias file lookup with CRLF, Unicode, asset success/fallback; retained slug/collision/filename tests in exporter; public snapshot tests retain partial-read behavior |
| CHG-06 | Export Run full/incremental/one/disabled cleanup, scan versus worker failure; existing cleanup hidden/subdirectory and asset tests retained |
| CHG-07 | Upload temporary Git staged/unstaged/untracked/Unicode paths and scoped discard; mode effects and conflict rejection; comparison followed by fresh paginated reads, changed children, title/read/delete/append failures; resolve list/diff/discard handlers |
| CHG-08 | LLM local request messages/model/temperature/JSON tests; chain/group/min/max and missing-journal cases; completion/write failure and per-page continuation; original direct-page failure test retained |
| CHG-09 | Render constructor rejects missing/invalid prerequisites; failed read then successful reuse; streaming/session byte comparison; overlapping renders serialize; close waits active asset, idempotence, no I/O after close and borrowed reader remains usable; upload factory once after nonempty discovery |
| CHG-10 | Public notionread/retry sources/tests retained unchanged; external-package transformer golden compiles and exercises public snapshot/render/future APIs |
| CHG-11 | Before/after top-level passing-test inventory and skip check; fixed full verifier still tests `./...`, including real failing-test controls and selector rejection |
| CHG-12 | README routes to command/configuration references; architecture and focused commands point to current owners; local documentation-link audit |
| CHG-13 | Collector Run collected A/candidates A+B writes B, later-page failure writes nothing, discovery failure writes nothing, no new IDs writes nothing, write failure logs/counts but returns success; original batch build/write failure tests retained |

Golden expectations reflect existing renderer output, including the `Page` label
on internal page links. The public renderer implementation was not changed to
make fixtures pass. Regression tests document the behavior preserved by the
refactor. They do not guarantee recovery from failures.

## Verification and review

- Environment: Go 1.26.2, Windows/amd64; module minimum and release targets unchanged.
- `go run ./scripts/verify`: passed tests without cache, vet, and build.
- Test inventory: all 58 original top-level tests retained; 81 passed at completion,
  with no skipped tests. The fixture-helper package has no standalone tests and is
  exercised by its consumers.
- Baseline/current binary comparison: eight offline flag/help/config/error/repeat
  cases matched exit code and output after executable-name/timestamp normalization.
- Local documentation-link audit and `git diff HEAD --check`: passed.
- Independent production migration review: no actionable findings. Compared
  moved function bodies, CLI/config ordering, resource/write lifetimes and embedded
  HTML against the baseline; independently ran app/CLI/config/exporter/upload tests.
- Independent final acceptance/evidence review: no actionable findings. It
  independently ran focused workflow, shared mechanism, golden and render-lifecycle
  tests. A noted overlapping-render coverage limit was closed with a dedicated
  test and rechecked before the review passed. No production fixes were requested.

Tests use local fixture servers and temporary repositories only. Normal Go
module/toolchain downloads can require network access. Live API behavior,
cross-platform runtime execution, exhaustive rendering and race freedom are not
claimed. The resolve server retains its existing process lifetime and has handler
coverage rather than a new shutdown mechanism.
