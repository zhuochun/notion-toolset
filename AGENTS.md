# Agent entry point

This repository is a Go CLI for Notion automation. Start with `git status --short`
and preserve unrelated changes. Run development commands from the repository root.

## Find the owner

- User setup and supported commands: [README.md](README.md).
- Runtime, focused checks, verification, and release procedure:
  [DEVELOPMENT.md](DEVELOPMENT.md).
- Current code responsibilities and change routes:
  [docs/architecture.md](docs/architecture.md). This describes implementation;
  it does not approve a redesign or establish intended behavior where code and
  user requirements disagree.
- YAML examples: `example/configs/`; their executable contract is
  `TestExampleConfigsMatchSchemaAndRequiredContract` in `internal/config/examples_test.go`.
- Accepted change intent comes from the current task or accepted review. Git
  history explains past changes; do not treat a proposed refactor as accepted.

Read the relevant code, documentation, and tests before editing. Update the linked
documentation when changing its commands, configuration, or ownership; avoid
copying it here.

## Work and evidence

- Use the focused checks in DEVELOPMENT.md during a change, then run
  `go run ./scripts/verify` before handoff. Report failures and untested scope.
- The verifier is the shared local/CI implementation. Keep its full-suite scope
  fixed; focused selectors belong in direct `go test` commands.
- Keep test fixtures local: HTTP test servers and temporary directories or Git
  repositories. Normal verification requires no Notion or LLM credentials.
- Preserve CLI flags, YAML keys/defaults, Markdown output, package import paths,
  and partial-failure behavior during a structural refactor. Intentional
  changes to those contracts require an explicit behavior decision.
- Live Notion/LLM runs, upload/discard operations, release publication, and remote
  writes need task authorization; setup and green checks do not grant it.

These workflow and authority rules are agent guidance. The verifier enforces only
the checks documented in DEVELOPMENT.md; it cannot enforce intent or prove live
workflow correctness.
