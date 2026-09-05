# Initial agent environment setup

Snapshot: 2026-09-05, baseline commit `c252305` (clean working tree).
Mode: **Retrofit** of an existing Go CLI, following the owner's request for initial
agentic repository setup before a separate refactor. This is historical setup
evidence; current commands belong to [DEVELOPMENT.md](../DEVELOPMENT.md).

## Decisions and owners

| Choice | Disposition | Reason and reversal/activation path |
| --- | --- | --- |
| Preserve Go module, root executable, CLI/YAML behavior, imports, release targets | Accepted existing constraints | Setup authority covers knowledge and fixed verification wiring, not production redesign. |
| Short root AGENTS router plus existing contributor guide and observed architecture map | Reversible default | Makes config/export work discoverable; reconcile or simplify these routes when ownership changes. |
| Go-based shared verifier | Reversible default | Uses the existing runtime on Windows and Linux; reverting the workflow/doc wiring restores the direct three-command path. No Bash/Make dependency added. |
| Config dispatch and export/read paths as representative work | Reversible default | Grounded in current tests and the upcoming refactor; rehearse discovery/checks without changing product behavior. |
| Application package extraction and error/cancellation changes | Deferred | Resume with an accepted concrete refactor scope. |
| Race/platform CI expansion and new application test oracles | Deferred | Requires a bounded verification change; current CI scope remains test/vet/build on Linux. |
| Autonomous jobs, hooks, learning registry | Deferred | Reopen for repeated accepted corrections or demonstrated recurring agent-work friction; use existing task/review evidence first. |

Instruction inspection found the supplied global Windows execution contract and
no root/ancestor AGENTS override or nested repository instructions at baseline.
No configured `project_doc` fallback was found in the inspected local config.
The new root AGENTS routes to existing authority; it does not duplicate the global
Windows contract or change tool permissions.

## Before and after

| Surface | Frozen baseline | Setup delta and limit |
| --- | --- | --- |
| Full evidence | Direct `go test -count=1 ./...`, `go vet ./...`, `go build ./...` passed locally; CI had the same three stages | Shared `go run ./scripts/verify` executes those exact commands in order and stops on failure. All four existing packages remain selected; adds the verifier package and its tests. |
| Test selection | 53 top-level test functions across four packages; no `t.Skip` calls found in existing tests | No existing test/production files changed. Five verifier tests added. The wrapper rejects arguments and nonempty effective GOFLAGS; direct focused commands remain supported. |
| Evidence omissions | No full live export/upload/LLM evidence; sparse Markdown and journal coverage; no CI race or native Windows/macOS runtime tests | Unchanged, explicitly documented rather than treated as covered by the new gate. |
| Exit behavior | Go commands return nonzero on failure; CI stops before later stages | Verifier preserves rejection and child diagnostics, returns nonzero, and stops before later stages; no claim to preserve each child's exact numeric exit code. |
| Discovery | README/DEVELOPMENT and code/tests existed, but no repository agent entry or ownership map | Root router connects task intent to current implementation and focused/full checks. |
| Delivery | Release workflow called reusable verify before cross-builds | Reusable workflow still owns enforcement, permissions, triggers, timeout, and release dependency; only command wiring changed. Hosted execution not run locally. |

## Representative evidence

Baseline environment: Go 1.26.2, Windows/amd64, CGO disabled; effective GOFLAGS
empty. The module's declared language minimum remains Go 1.25.0. Earlier coverage
probe: root 20.5%, notionread 85.0%, retry 56.0%, transformer 5.2%; these are
package-local statement measurements, not aggregate behavioral adequacy.

- Config path: AGENTS -> DEVELOPMENT focused route -> `main.go`,
  `main_test.go`, and `example/configs/`. Oracle: supported command mapping,
  validation ordering, error wrapping, and example schema/template assertions.
- Export path: AGENTS -> architecture map -> `export.go`, `notionread/doc.go`,
  relevant tests -> focused export/read checks -> full verifier. Oracle: existing
  asset/filename/cleanup assertions plus read pagination/snapshot assertions.
- Negative control: verifier test creates a separate temporary Go module whose
  `TestProbe` calls `t.Fatal("verification negative control")`. The real Go test
  command fails, the original diagnostic is retained, and vet/build do not run.
  A contrasting passing module reaches all three stages. Selector and wrong-root
  cases check actionable rejection. No production mutation is needed.

## Final rehearsal results

| Exercise | Result | Validity limit |
| --- | --- | --- |
| `go mod download` then `go run ./scripts/verify` | Passed: tests in five packages, vet, build; all five verifier tests passed, including the real failing-fixture negative control | Local Go 1.26.2 Windows/amd64 only |
| Fresh source snapshot containing tracked files plus this setup's new files; Notion/LLM environment credentials removed; same setup/full commands | Passed | Reused installed Go and module/build caches; not a fresh OS or cold-cache download trial |
| Documented config, export, and notionread/retry focused commands | Passed | Existing assertions only; no application changes made to rehearse the path |
| Distinct fresh-context agent: config dispatch and export cleanup discovery | Both routes reached the relevant owners and passing focused checks; no stale/broken route found among inspected sources | Retrieval rehearsal, not an independent full implementation review |
| `git diff HEAD --check`, inspection of new files, and diff against existing Go files/module/README/release workflow | Passed; existing production code, tests, dependencies, and release wiring outside the reusable verifier remain unchanged | New verifier files extend package selection; a passing gate does not prove repository cleanliness |
| Hosted CI, minimum Go 1.25 runtime, native Linux/macOS execution, live APIs, race detector | Not run | Follow up only when the relevant task needs that evidence |

The fresh consumer loaded `AGENTS.md`, `README.md`, `DEVELOPMENT.md`,
`docs/architecture.md`, `main.go`, `main_test.go`, `export.go`, `export_test.go`,
`example/configs/export.yaml`, `go.mod`, `scripts/verify/main.go`, and
`.github/workflows/verify.yml`. It used symbol searches to find the scenario
owners and did not need the historical setup record. It correctly surfaced the
runtime-versus-example YAML validation distinction and partial export failure
limit without treating either as permission to change behavior.

The local and CI copies of the three-command recipe now route through one
implementation. Underlying Go commands remain available; no pre-existing path
was deleted, and no commit, push, remote setting, deployment, or release action
was performed. No setup slice remains blocked. Reopen this setup if a command or
owner moves, a focused route goes stale, or an accepted correction demonstrates
a reusable environment gap. The application refactor remains the next separate
scope decision.
