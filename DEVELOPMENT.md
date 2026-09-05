# Development

This document is for contributors and maintainers of `notion-toolset`.

## Local Dev Setup

Requirements:

- Go 1.25 or later (the language minimum is owned by `go.mod`)
- Git for checkout and upload development; no Bash or Make requirement for verification

Setup:

```bash
git clone https://github.com/zhuochun/notion-toolset.git
cd notion-toolset
go mod download
go run ./scripts/verify
```

Run from source:

```bash
export NOTION_TOKEN="your-secret-token"
go run . --cmd=daily-journal --config=./example/configs/journal-daily.yaml
```

Build locally:

```bash
go build .
```

## Repository Layout

- `AGENTS.md`: agent task router and scope/evidence guidance
- `docs/architecture.md`: current code ownership and known test boundaries
- `scripts/verify/`: shared local and CI verification implementation
- `example/configs/`: sample YAML configs for each command
- `example/workflow/`: GitHub Actions examples that build from source
- `example/gitlab-ci.yml`: GitLab CI example that downloads a released binary
- `.github/workflows/release-binaries.yml`: release artifact builder

## Checks and Diagnosis

Run commands from the repository root. The full gate is:

```text
go run ./scripts/verify
```

It runs `go test -count=1 ./...`, `go vet ./...`, and `go build ./...` in order,
streams their diagnostics, and exits nonzero at the first failure. It accepts no
arguments and requires empty effective `GOFLAGS`, including persisted `go env -w`
settings, so a focused test selector cannot silently narrow verification. Use
`go env GOFLAGS` to diagnose this rejection. Review and clear the relevant setting
before retrying. The underlying direct Go commands remain available.

No Notion/LLM credentials are needed for tests. Initial module/toolchain download
may need network access; test API fixtures use local HTTP servers. Verifier tests
use temporary modules with one intentionally failing test to prove failure
propagation. Its expected failure is captured by a passing parent test.

| Work intent | Focused command | What it establishes |
| --- | --- | --- |
| Executable, config and routing | `go test -count=1 . ./internal/cli ./internal/app ./internal/config` | Binary help/exits, invocation isolation, repeat/multi selection, validation order and examples |
| Export files, assets and lifetime | `go test -count=1 ./internal/exporter` | Filename/cleanup contracts, scan versus worker failure, render-session closure and asset completion |
| Upload and resolve UI | `go test -count=1 ./internal/upload` | Temporary Git repositories, mode effects, fresh pre-delete pagination/failure order and embedded HTTP handlers |
| Journals, selection and collection | `go test -count=1 ./internal/journal ./internal/flashback ./internal/duplicate ./internal/collector` | Date boundaries, fallback, OR keys, discovery/scan-before-write and batch continuation |
| LLM and shared writes | `go test -count=1 ./internal/llm ./internal/notionops` | Local completions, chain/group/filter behavior, text/JSON, templates and batching |
| Read pagination/snapshots and retry | `go test -count=1 ./notionread ./retry` | Local API fixtures, cancellation, concurrency, backoff |
| Markdown transformation | `go test -count=1 ./transformer` | Nested blocks, properties, aliases, Unicode, assets/fallback and existing child-database behavior |
| Verification wiring | `go test -count=1 ./scripts/verify` | Real test failure propagation, argument/GOFLAGS rejection, success stages |
| Coverage diagnosis | `go test -count=1 -cover ./...` | Statement coverage, not behavioral adequacy |

Focused checks supplement the full gate. Format changed Go files with `gofmt -w`
and inspect `git diff HEAD --check` before handoff; these are contributor checks,
not additional enforced verifier stages. Inspect `git status --short` as well:
Git diff checks do not include untracked files.

For a failure, retain the command, first failing package/test, diagnostic, runtime
(`go version`), and relevant diff. Resolve the failure or report the blocked check;
do not turn it into a skip or narrow the full gate. No automatic retries or
background jobs are installed. Interrupt with Ctrl+C and rerun explicitly; normal
Go test timeouts and the CI job's 15-minute timeout bound test/CI execution.

The gate does not establish live API compatibility, full export/upload success,
race freedom, exhaustive Markdown rendering, or release-platform runtime behavior.
Runtime tests currently run on the CI Linux host; cross-compiling release binaries
is not equivalent to testing Windows/macOS. Live workflow execution needs explicit
task scope, especially upload/discard and remote writes.

## Agent Setup and Follow-up

The initial setup uses existing behavior as its baseline; see
[the setup evidence record](docs/agent-setup.md). Keep user instructions in
README.md, contributor procedures here, observed ownership in the architecture
map, and executable checks in their code/test owners. When those sources disagree
about intended behavior, surface the conflict in the task instead of deciding it
from implementation alone.

Renewal automation and a separate learning registry are deferred. Activate a small
renewal path in the existing task/review workflow when an accepted correction
recurs or repeated agent work demonstrates a stale route or verification gap.
Record the correction and evidence there, repair the lowest durable owner, trial
the source case plus a contrasting case, and document the guardrail result and
reversal signal. A green trial does not authorize commits, remote changes, or
release publication.

## Release Binaries

GitHub Releases are used to publish downloadable binaries.

Workflow:

- File: [`.github/workflows/release-binaries.yml`](.github/workflows/release-binaries.yml)
- Trigger: GitHub Release `published`
- Verification: the shared `verify` workflow must pass before release binaries are built
- Output targets:
  - `darwin/amd64`
  - `darwin/arm64`
  - `linux/amd64`
  - `linux/arm64`
  - `windows/amd64`
  - `windows/arm64`

Artifacts:

- `.tar.gz` for macOS and Linux
- `.zip` for Windows
- `.sha256` checksum file for each artifact

Release process:

1. Push a version tag such as `v1.2.3`.
2. Publish a GitHub Release for that tag.
3. Wait for the `release-binaries` workflow to finish.
4. Verify the assets on the release page.

Pull requests and pushes to `main` run the same tests, vet, and build checks through [`.github/workflows/verify.yml`](.github/workflows/verify.yml).

## Notes

- User-facing setup should stay in `README.md`.
- Source-based setup and maintainer workflows should stay in this file.
