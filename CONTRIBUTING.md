# Contributing

Thanks for helping improve the Home Assistant packaging.

## Before opening a pull request

- Keep changes focused on the HAOS add-on unless an upstream application change is necessary.
- Do not include secrets, cookies, personal data, runtime databases, logs, or generated binaries.
- Preserve upstream MIT license and attribution notices.
- Update CHANGELOG.md and README.md when behavior or configuration changes.
- Add or update tests for application behavior changes.

## Required checks

The GitHub Actions workflow must pass:

- go test ./...
- go vet ./...
- bash -n run.sh
- AMD64 and ARM64 Docker builds

Do not publish images from unreviewed pull requests.
