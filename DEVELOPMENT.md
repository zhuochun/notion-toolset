# Development

This document is for contributors and maintainers of `notion-toolset`.

## Local Dev Setup

Requirements:

- Go 1.25 or later

Setup:

```bash
git clone https://github.com/zhuochun/notion-toolset.git
cd notion-toolset
go mod download
go test ./...
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

- `example/configs/`: sample YAML configs for each command
- `example/workflow/`: GitHub Actions examples that build from source
- `example/gitlab-ci.yml`: GitLab CI example that downloads a released binary
- `.github/workflows/release-binaries.yml`: release artifact builder

## Release Binaries

GitHub Releases are used to publish downloadable binaries.

Workflow:

- File: [`.github/workflows/release-binaries.yml`](.github/workflows/release-binaries.yml)
- Trigger: GitHub Release `published`
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

## Notes

- User-facing setup should stay in `README.md`.
- Source-based setup and maintainer workflows should stay in this file.
