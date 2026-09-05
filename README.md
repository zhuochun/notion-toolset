# Notion Toolset

A small CLI for recurring Notion workflows: create daily and weekly journals,
resurface older pages, find duplicates, collect references, export Markdown,
upload local edits, and run LLM prompts on page content.

## Quick start

1. [Create a Notion integration](https://developers.notion.com/docs/getting-started)
   and share your target databases and pages with it.
2. Download the binary for your system from
   [GitHub Releases](https://github.com/zhuochun/notion-toolset/releases).
3. Copy a matching [example config](example/configs/) into a local `configs/`
   folder. Replace the sample Notion IDs and adjust its settings.
4. Set your integration token and run a command.

**PowerShell**

```powershell
$env:NOTION_TOKEN="your-secret-token"
.\notion-toolset.exe --cmd=daily-journal --config=.\configs\journal-daily.yaml
```

**macOS / Linux**

```bash
export NOTION_TOKEN="your-secret-token"
./notion-toolset --cmd=daily-journal --config=./configs/journal-daily.yaml
```

Choose `daily-journal`, `weekly-journal`, `flashback`, `duplicate`, `collector`,
`export`, `upload`, or `llm`. Run `--help` for flags.

LLM commands also require `DOT_OPENAI_KEY`. Upload uses your exporter config and
runs inside a Git repository; its default mode is `dry-run`. Read the
[upload modes and recovery limits](docs/commands.md#upload-modes-and-recovery-limits)
before applying local edits to Notion.

## Scheduled runs

Use the [GitLab CI example](example/gitlab-ci.yml) or
[GitHub Actions examples](example/workflow/). Set your command/config path and
store tokens as CI secrets. Run once manually before adding a schedule.

## Documentation

- [Commands](docs/commands.md): flags, workflow behavior, upload modes and failures.
- [Configuration](docs/configuration.md): YAML fields, defaults, templates and environment variables.
- [Development](DEVELOPMENT.md): source builds, tests and releases.
- [Architecture](docs/architecture.md): package responsibilities and change routes.
