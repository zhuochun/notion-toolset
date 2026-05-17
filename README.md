# Notion Toolset

Automate recurring Notion workflows with a small command-line tool.

`notion-toolset` connects to the Notion API, reads a YAML config, and runs one focused workflow at a time. Most users should start with a released binary and either run it locally or from GitLab CI.

## Features

- Daily journal creation
- Weekly journal creation
- Duplicate page detection
- Flashback resurfacing for older pages
- Page collection into a destination block
- Markdown export and backup of database pages
- Reverse upload of changed local markdown exports back to Notion
- LLM-driven summarization or prompt execution on page content

## Setup Flow

Choose one of these:

1. Local run first
2. GitLab CI first

In both cases you need the same Notion setup:

1. Create a Notion integration by following the [official guide](https://developers.notion.com/docs/getting-started).
2. Copy the integration token.
3. Share the target Notion databases and pages with that integration.

If the integration cannot see a database or page, the command will fail.

## Local Run

This is the fastest way to get a workspace running.

1. Create an empty folder for your automation files.
2. Download the correct binary for your machine from the GitHub Releases page.
3. Create a `configs/` folder.
4. Copy one of the example configs from `example/configs/` into your own `configs/` folder.
5. Edit the config with your real Notion IDs.
6. Set `NOTION_TOKEN` in your shell.
7. Run the binary with `--cmd` and `--config`.

PowerShell example:

```powershell
$env:NOTION_TOKEN="your-secret-token"
.\notion-toolset.exe --cmd=daily-journal --config=.\configs\journal-daily.yaml
```

macOS/Linux example:

```bash
export NOTION_TOKEN="your-secret-token"
./notion-toolset --cmd=daily-journal --config=./configs/journal-daily.yaml
```

## GitLab CI Run

If you want the job to run on a schedule, start with a small GitLab repository that contains only your config files and CI definition.

Recommended folder structure:

```text
your-notion-automation/
  .gitlab-ci.yml
  configs/
    journal-daily.yaml
```

Setup steps:

1. Create an empty GitLab repository.
2. Copy a config example from `example/configs/` into `configs/`.
3. Copy [`example/gitlab-ci.yml`](example/gitlab-ci.yml) into your repo as `.gitlab-ci.yml`.
4. Update the config path, command, and release version in `.gitlab-ci.yml`.
5. Add `NOTION_TOKEN` in GitLab under `Settings` > `CI/CD` > `Variables`.
6. Run the pipeline manually once, then add a schedule if needed.

The GitLab example downloads a released Linux binary, so the CI job does not need Go installed.

## Config Examples

Use the example that matches the feature you want:

- `example/configs/journal-daily.yaml`
- `example/configs/journal-weekly.yaml`
- `example/configs/duplicate.yaml`
- `example/configs/flashback.yaml`
- `example/configs/collector.yaml`
- `example/configs/export.yaml`
- `example/configs/llm-summary.yml`
- `example/configs/llm-summary-json.yml`
- `example/configs/llm-flashback.yaml`

Common values you will need to replace:

- `databaseID`
- destination page or block IDs such as `duplicateDumpID` or `collectDumpID`
- query JSON blocks
- output directory for exports
- prompt text for `llm`

If you need a Notion ID, copy it from the page or database URL. The tool expects the 32-character Notion ID.

## Commands

Each run uses one command plus one config file.

- `daily-journal`
- `weekly-journal`
- `duplicate`
- `flashback`
- `collector`
- `export`
- `upload`
- `llm`

Example:

```bash
./notion-toolset --cmd=export --config=./configs/export.yaml
```

Reverse upload uses the same exporter config and only scans uncommitted files under `exporter.directory`.

```bash
./notion-toolset --cmd=upload --config=./configs/export.yaml
./notion-toolset --cmd=upload --config=./configs/export.yaml --mode=discard
./notion-toolset --cmd=upload --config=./configs/export.yaml --mode=upload
./notion-toolset --cmd=upload --config=./configs/export.yaml --mode=resolve --workspace=https://www.notion.so/your-workspace
./notion-toolset --cmd=upload --config=./configs/export.yaml --mode=resolve --workspace=your-workspace
./notion-toolset --cmd=upload --config=./configs/export.yaml --mode=resolve --workspace=your-workspace.notion.site
./notion-toolset --cmd=upload --config=./configs/export.yaml --mode=resolve --port=17889
```

`resolve` starts a local web UI with file list and unified diffs on `127.0.0.1:17889` by default (override with `--port`). `workspace` accepts full URL, host (`your-workspace.notion.site`), or workspace path segment (`your-workspace`). It loads file list first, and compares/exports Notion content only when you select a file. Use it to open the page in Notion and discard a local file from the browser.

## Troubleshooting

- Empty or unauthorized results usually mean the integration was not shared to the target database or page.
- Startup failures usually mean `NOTION_TOKEN` is missing.
- Export failures can happen when the output directory in the config does not exist yet.
- YAML errors usually mean indentation or quoting is wrong in the config file.

## Development

Contributor setup, source builds, and release workflow details are documented in [`DEVELOPMENT.md`](DEVELOPMENT.md).
