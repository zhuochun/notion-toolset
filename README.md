# Notion Toolset

A set of tools to automate my [Notion](https://www.notion.so/) workflow using [Notion API](https://developers.notion.com/reference) and GitHub Action.

Build with [dstotijn/go-notion](https://pkg.go.dev/github.com/dstotijn/go-notion).

## Setup

- Follow [the official guide](https://developers.notion.com/docs/getting-started) to create your own Notion integration.
  - Take note of your Notion API token.
  - Share your databases with the integration afterwards.
- Create a new GitHub repo:
  - Setup your Notion API token:
    - Go `Settings` > `Secrets` > `Actions`.
    - Create a `New repository secret`, using the name `NOTION_TOKEN` and the value of the actual token.
  - Setup your GitHub Actions:
    - Follow [the official guide](https://docs.github.com/en/actions/quickstart).
    - You can reference to the `example/workflow/` for examples.
  - Setup your notion-toolset Configs:
    - You need to create yaml config files for each tool and use them with `--config=path/to/config.yml`.
    - You can reference to the `example/configs/` for examples.

## Tools

- `--cmd=daily-journal`: Create empty daily pages in a database
- `--cmd=weekly-journal`: Create empty weekly pages in a database
- `--cmd=duplicate`: Find duplicated titles in a database and write them to a block
- `--cmd=flashback`: Resurface some random pages in a database and write them to a block
- `--cmd=collector`: Collect a set of pages and write them to a block
- `--cmd=cluster`: Cluster pages in a database by their titles (basic)
- `--cmd=export`: Export pages in a database to markdown files (text content only)