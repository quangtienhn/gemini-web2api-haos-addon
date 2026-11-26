# Gemini Web2API — Add-on documentation

## Overview

This Home Assistant app packages zexadev/gemini-web2api-go. It provides an OpenAI-compatible API, Gemini-native API routes, an MCP endpoint, and an authenticated Admin UI.

This is an unofficial community integration and is not affiliated with Google.

## Installation

Add the repository URL after the GitHub repository and GHCR package have been made public:

https://github.com/quangtienhn/gemini-web2api-haos-addon

In Home Assistant, open Settings → Apps → Install app → menu → Repositories, then add the repository URL and install Gemini Web2API.

## Required configuration

| Option | Guidance |
|---|---|
| admin_password | Required. Set a unique password of at least 8 characters for the Admin UI. |
| adapter_api_key | Optional. Separate key for the Image Adapter; generated and persisted automatically when empty. |
| api_key | Required. Separate key for API clients; use at least 8 characters. |
| disable_media_generation | Keep true initially. |
| disable_url_fetch | Keep true initially. |
| disable_account_pool | Keep true initially. |
| disable_proxy_pool | Keep true initially. |
| cors_allow_origins | Keep empty unless a trusted browser origin is required. |

The service exits before startup if the Admin password or API key is missing or shorter than 8 characters. The Admin UI login uses the configured Admin password; legacy token login remains supported only by deployments that have not entered password mode.

## Network endpoints

- API base URL: http://HOME_ASSISTANT_IP:8083/v1
- Health check: http://HOME_ASSISTANT_IP:8083/health
- Admin UI: http://HOME_ASSISTANT_IP:8083/admin
- Ingress is disabled; open the Admin UI directly at `http://HOME_ASSISTANT_IP:8083/admin`.

The Admin UI remains protected by its own authentication and is reachable on the mapped LAN port.

## Client setup

Use the API key configured above as a Bearer token.

- Base URL: http://HOME_ASSISTANT_IP:8083/v1
- API key: the configured API key
- Model: select a model supported by the upstream application.

## Persistent data and backup

Application state is stored under /data. The add-on uses cold backup mode so the application is stopped during backup, reducing SQLite consistency risk. Keep regular Home Assistant backups and test restore before relying on this service.

The cookie encryption key is stored in /data. Losing that key can make encrypted stored cookies unreadable. Do not delete or replace it casually.

## Security notes

- Do not expose port 8083 to the public internet.
- Use unique, strong Admin and API keys.
- Do not commit Google cookies or session exports.
- URL fetching and media generation are disabled by default.
- Keep account and proxy pools disabled until separately reviewed.
- Do not enable features unless you understand their security and upstream-service implications.

## Build and release

The repository-level workflow is at ../.github/workflows/build-haos-addon.yml.

A build-only run must pass Go tests, go vet, shell syntax validation, and Docker builds for AMD64 and ARM64. Publishing is manual and opt-in. The version in this file's parent config.yaml must match the image tag.

## Upstream

Application source and feature documentation: https://github.com/zexadev/gemini-web2api-go

This add-on repository maintains the Home Assistant packaging and downstream hardening separately.
