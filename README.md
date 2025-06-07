# Notion Toolset

A set of tools to automate my [Notion](https://www.notion.so/) workflow using [Notion API](https://developers.notion.com/reference) and GitHub Action.

Build with [dstotijn/go-notion](https://pkg.go.dev/github.com/dstotijn/go-notion).

Requires **Go 1.20** or later.

## Repo Setup

Clone the repository and download the Go dependencies:

```bash
git clone https://github.com/zhuochun/notion-toolset.git
cd notion-toolset
go mod download
go vet ./...
```

You can then build the tool with `go build` or run commands directly using
`go run main.go`.

## Setup

- Follow [the official guide](https://developers.notion.com/docs/getting-started) to create your own Notion integration.
  - Take note of your Notion API token somewhere.
  - Share your databases with the integration afterwards.
- Create a new GitHub repo:
  - Setup your Notion API token:
    - Go `Settings` > `Secrets` > `Actions`.
    - Create a `New repository secret`, using the name `NOTION_TOKEN` and the value of the actual token.
  - Setup your GitHub Actions:
    - Follow [the official guide](https://docs.github.com/en/actions/quickstart).
    - Refer to `example/workflow/` for examples.
  - Setup your notion-toolset Configs:
    - You need to create yaml config files for each tool and use them with `--config=path/to/config.yml`.
    - Refer to `example/configs/` for examples.

## Tools

- `--cmd=daily-journal`: Create empty daily pages (YYYY-MM-DD) in a database
- `--cmd=weekly-journal`: Create empty weekly pages (YYYY-MM-DD/YYYY-MM-DD) in a database
- `--cmd=duplicate`: Find duplicated pages with a same titles in a database, and write them inside a block. Optionally checks a URL property for broken links when configured
- `--cmd=flashback`: Resurface some random pages in a database, and write them inside a block/or today's journal page
- `--cmd=collector`: Find new pages that have not been collected, and write them inside a block
- `--cmd=export`: Export/backup pages in a database to markdown files (text and images)
- `--cmd=llm`: Run a GPT prompt on a page content
  - Set `groupExec: true` in the LLM config to combine all pages in a single request
  - Optional `groupJournalID` writes the group result to today's journal page when set
