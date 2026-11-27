## [4.35.2-dev] - 2026-11-27

### Added
- P0 performance instrumentation: per-request trace IDs, structured `[perf]` summary logs, prompt/tool/body metadata (no prompt/secret logging), upstream/streaming milestones, tool-fence hold metrics, retry/empty-frame counters, and requested/resolved/upstream model correlation.
- Instrumentation hooks in chat/responses handlers, SSE writer, tool-fence gate, and Gemini upstream request path for HAOS runtime diagnosis.

### Notes
- Development artifact only. No production config changes, no GHCR publish, no GitHub release/tag.
- Go/Docker validation may be NOT TESTED in environments without Go/Docker toolchain.

# Changelog

All notable changes to this HAOS add-on packaging are documented here.

## 4.35.1 - Admin password login UI compatibility

### Fixed
- Admin UI now displays an Admin Password field and submits the `password` credential expected by password mode.
- Login submits the same credential in both `password` and `token` fields so legacy token-mode deployments remain compatible; password mode still takes precedence in the backend.
- Updated Admin UI login error text and English translation.
- Set the `admin_password` metadata default to an empty string instead of null so Supervisor can validate the required password option before the user configures it.
- Updated README and DOCS configuration guidance to describe Admin Password mode and direct LAN access.

### Changed
- Bumped add-on version to 4.35.1. This is a source change only; no image has been built or published.


## 4.35.0 - R3 finalization: single-state auth, recovery procedure, release package

### Changed
- password/auth: simplified to single authoritative state file /data/admin_auth.json (v1, locked, argon2id hash) with atomic write; legacy hash/lock/tx/recovery files migrated on startup.
- password/auth: unknown/corrupt/unreadable auth state fails closed; no legacy admin_token fallback when password mode is active or unknown.
- recovery: manual out-of-band reset via /data/admin_password.reset marker + dmin_password option on next startup; no HTTP recovery endpoint.

### Fixed
- run.sh: chowns admin_auth.json and legacy auth marker files under /data.
- version bump to 4.35.0 for release package.

## 4.34.4 - V2.1.2-R2 security consistency & test completion

### Fixed
- password.go: passwordModeLocked() now fail-closed on unknown lock/marker state (Stat errors other than NotExist return locked).
- password.go: added tx/recovery markers; setAdminPasswordAtomic() uses tx marker and recovery marker to avoid undefined auth state on rollback/sync failures.
- password.go: FS abstraction (passwordFS) added for fault-injection tests; writeFileAtomicFS/syncDirOS used.

### Security notes
- No legacy admin_token fallback when lock state is unknown.
- No automated HTTP recovery endpoint added; manual out-of-band recovery required.

## 4.34.3 - V2.1.2-R1 security remediation

### Fixed
- run.sh: per-process env isolation enforced with `env -i` allowlists for Web2API and adapter (prevents cross-process secret leakage via /proc/<pid>/environ).
- password.go: strict PHC Argon2id parser with explicit supported bounds; status classification (valid/empty/corrupt/unsupported/unreadable/missing); verify rejects malformed/unsupported input before Argon2 KDF.
- password.go: atomic password hash persistence via temp+fsync+rename; lock/hash consistency handling on failure.
- adapter/app.py: SIGTERM/SIGINT handlers trigger graceful HTTP server shutdown (shutdown called from non-serve_forever thread).

### Security notes
- Adapter must not receive WEB2API_ADMIN_PASSWORD/TOKEN/API_KEY; Web2API must not receive ADAPTER_API_KEY/UPSTREAM_API_KEY.
- Password mode lock remains fail-closed on corrupt/unsupported/unreadable hash; no admin_token fallback.

## 4.34.2 - V2.1.2 security & runtime hardening

### Added
- password.go: read `WEB2API_ADMIN_PASSWORD` env instead of `ADMIN_PASSWORD` for env mapping consistency with run.sh.
- adapter/app.py: `base64.b64decode(..., validate=True)` replaces `validate=False` to enforce base64 format validation.
- adapter/adapter.py: `parse_data_url` uses `base64.b64decode(..., validate=True)` for data URL decoding.

### Fixed
- run.sh: confirmed `WEB2API_ADMIN_PASSWORD` is the sole admin password env exported to Web2API process; adapter receives no admin credential.
- password.go: `ensureAdminPassword()` now reads `WEB2API_ADMIN_PASSWORD`; `TestInitAdminPasswordFromEnv` updated accordingly.
- adapter B6: base64 decoding now validates input format, rejecting malformed base64 strings.

## 4.34.1 - V2.1.1 runtime safety hardening

### Added
- run.sh: `trap` for SIGTERM/SIGINT with graceful shutdown (SIGTERM → grace period → SIGKILL → reap children).
- run.sh: continuous health polling of both Web2API (8083) and adapter (8084) endpoints with retry threshold.
- run.sh: adapter restart with limit (max 3), then exit nonzero if unhealthy.
- password.go: `passwordHashStatus()` distinguishes valid/empty/corrupt/unsupported/unreadable/missing hash file.
- password.go: `passwordModeLocked()` via `/data/admin_password.lock` — once a password is configured, legacy admin_token bypass remains disabled even if the hash file is later corrupted or deleted.
- password.go: `setAdminPassword()` creates the lock file; `hasAdminPassword()` returns true only for valid PHC argon2id hash.
- admin.go: `authOK()`/`bearerAuthOK()` enforce password mode when lock file exists (hash corrupt/missing does not fall back to token bypass).
- admin.go: `handleAdminLogin()`/`handleAdminPassword()` enforce password mode when lock file exists.

### Fixed
- run.sh: secret isolation — Web2API receives only `WEB2API_*` env; adapter receives only `ADAPTER_*`/`UPSTREAM_*` env. `ADMIN_PASSWORD`, `ADMIN_TOKEN`, `API_KEY` are no longer exported to the adapter process.
- run.sh: Web2API and adapter started via `su-exec gemini:gemini`; ownership of `/data` and secret files set before dropping privileges.
- Dockerfile: HEALTHCHECK operator precedence fixed — `(wget 8083 && wget 8084) || exit 1` instead of `wget 8083 || exit 1 && wget 8084 || exit 1`.
- config.yaml: description no longer mentions Ingress (`ingress: false`).

## 4.34.0 - V2.1 remediation: non-root runtime, adapter key, startup fail-closed

### Added
- Gemini Image Adapter (`adapter/`) runs as a second process on port 8084, exposing OpenAI Images-compatible `POST /v1/images/generations`, `GET /v1/models`, `GET /health`.
- Adapter parses Markdown Data URI from Gemini Web2API `choices[0].message.content` and returns `data[0].b64_json`.
- Separate `adapter_api_key` option (distinct from client `api_key`), generated once and persisted in `/data/adapter_api_key`.
- Admin password (`admin_password`) with Argon2id hash persisted in `/data/admin_password.hash`; password mode disables internal `admin_token` bypass.
- `handleAdminPassword` endpoint for password rotation (authenticated).
- run.sh runs both Web2API and adapter as unprivileged `gemini:gemini` via `su-exec`.
- Docker HEALTHCHECK checks both 8083 (Web2API) and 8084 (adapter).

### Fixed
- run.sh: adapter startup is now fail-closed (exits if adapter unhealthy); repeated adapter restarts abort.
- run.sh: no blank line in jq command continuation (carried from 4.33.2).
- run.sh: `cors_allow_origins` normalized to valid JSON array.
- adapter: upstream model uses configured `UPSTREAM_MODEL` (default `gemini-image`), not hardcoded `gemini-3.1-flash-image`.
- adapter: strict Base64 validation (`validate=True`) on data URL decode.
- adapter: magic-bytes validation for PNG/JPEG.

### Changed
- Ingress disabled (`ingress: false`); Admin UI accessed directly via LAN `http://IP-HA:8083/admin` (`webui` URL).
- Ports 8083 and 8084 exposed.
- Version bumped 4.33.2 → 4.34.0 (4.33.2 already published).

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
