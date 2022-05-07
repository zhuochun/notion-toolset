# Notion Toolset

A set of tools to automate my [Notion](https://www.notion.so/) workflow using [Notion API](https://developers.notion.com/reference) and GitHub Action.

Build with [dstotijn/go-notion](https://pkg.go.dev/github.com/dstotijn/go-notion).

## Setup

- Follow the guide to create your own Notion integration.
- Setup up the environment variable `NOTION_TOKEN`.

## Tools

- `--cmd=daily-journal`: Create empty daily pages in a database
- `--cmd=weekly-journal`: Create empty weekly pages in a database
- `--cmd=duplicate`: Find duplicated titles in a database and write them to a block
- `--cmd=flashback`: Resurface some random pages in a database and write them to a block
- `--cmd=collector`: Collect a set of pages and write them to a block
- `--cmd=cluster`: Cluster pages in a database by their titles (basic)
- `--cmd=export`: Export pages in a database to markdown files (text content only)