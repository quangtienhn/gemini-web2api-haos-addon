# Changelog

All notable changes to this HAOS add-on packaging are documented here.

## 4.33.2 - Fix jq command line continuation

### Fixed
- run.sh: remove blank line inside jq command continuation that broke the filter expression.
- run.sh: normalize cors_allow_origins to a valid JSON array.

## 4.33.1 - Fix run.sh CORS normalization

### Fixed
- run.sh: normalize cors_allow_origins to a valid JSON array before passing to jq.

## 4.33.0 — Initial downstream package

- Add Home Assistant Supervisor add-on metadata and Ingress configuration.
- Add multi-architecture Dockerfile for AMD64 and ARM64.
- Run the service as an unprivileged user.
- Require a strong Admin token at startup.
- Default media generation, URL fetching, account pool and proxy pool to disabled.
- Use cold backups for SQLite consistency.
- Add GitHub Actions validation and opt-in GHCR publishing.
- Pass the API key through the supported API_KEY environment variable instead of a process command-line argument.
- Retain upstream MIT license and attribution.
