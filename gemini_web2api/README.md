# Gemini Web2API

Unofficial Gemini Web API proxy packaged as a Home Assistant OS add-on.

This app exposes an OpenAI-compatible API and an authenticated Admin UI (direct LAN access; Ingress disabled).

## Before starting

- Configure a unique Admin password of at least 8 characters.
- Configure a separate API key for clients.
- Keep media generation, URL fetching, account pool and proxy pool disabled until you explicitly need them.
- Do not expose TCP port 8083 to the public internet.

## Documentation

See DOCS.md for installation, configuration, endpoints, build and security guidance.

Upstream application: https://github.com/zexadev/gemini-web2api-go
