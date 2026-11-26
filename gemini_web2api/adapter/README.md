# Gemini Image Adapter

Small standalone service that exposes an OpenAI Images API compatible endpoint
backed by a Gemini image model served through CLIProxyAPI (Antigravity OAuth).

Home Assistant `Local OpenAI LLM` v1.14.0 `ai_task.generate_image` calls
`POST /v1/images/generations` with `response_format="b64_json"`. CLIProxyAPI does
not serve `gemini-3.1-flash-image` on that endpoint, but it does generate images
through `/v1/chat/completions`. This adapter bridges the two.

## Endpoints

- `GET /health` — liveness.
- `GET /v1/models` — lists the configured upstream model (for HA setup/validation).
- `POST /v1/images/generations` — OpenAI Images API compatible (b64_json output).

Image editing (`/v1/images/edits`) is **not** supported by this adapter yet.

## Run locally

```bash
cd gemini-image-adapter
cp .env.example .env
# edit .env: set ADAPTER_API_KEY and UPSTREAM_API_KEY
python3 app.py
```

Or:

```bash
./scripts/run.sh
```

## Configuration (.env)

See `.env.example`. Key variables:

- `ADAPTER_HOST`, `ADAPTER_PORT` — listen address (default `0.0.0.0:8099`).
- `ADAPTER_API_KEY` — key HA will use. Required unless `ALLOW_UNAUTHENTICATED=1`.
- `UPSTREAM_BASE_URL` — CLIProxyAPI base URL (default `http://192.168.1.133:8317/v1`).
- `UPSTREAM_API_KEY` — CLIProxyAPI access key.
- `UPSTREAM_MODEL` — model id (default `gemini-3.1-flash-image`).
- `UPSTREAM_TIMEOUT` — seconds (default 120).
- `MAX_REQUEST_BYTES`, `MAX_RESPONSE_BYTES` — safety limits.

## Security notes

- The adapter has its own API key; the CLIProxyAPI key never leaves the adapter.
- No Authorization header, OAuth token, or base64 image is logged.
- Request body and upstream response size are capped.
- Bind to LAN only in production; do not expose to the public internet.

## Tests

```bash
pip install -r requirements-dev.txt
pytest -q
```

## Home Assistant setup (after deployment)

1. In HA, add a new `Local OpenAI LLM` integration entry (or reconfigure) pointing at the adapter base URL, e.g. `http://192.168.1.133:8099/v1`, with `ADAPTER_API_KEY`.
2. Create a dedicated AI Task subentry (image generation only) and select model `gemini-3.1-flash-image`.
   Do not reuse this subentry for `generate_data` — image models are not chat models.
3. Call `ai_task.generate_image` with that entity.

## Limitations

- Single image per request (`n=1`). `n > 1` returns HTTP 400.
- Image size is determined by Gemini (observed 1408x768 JPEG); the `size` field is accepted but not enforced.
- `response_format` is always returned as `b64_json` regardless of requested format.
- No image editing / attachments support.
- No streaming.
- Upstream model is fixed by adapter config; the client `model` field is accepted but not used for routing.

## Rollback

Stop and remove the adapter service; HA entries pointing at it will fail. Re-point HA to a working image provider or remove the image AI Task subentry. CLIProxyAPI is never touched by this adapter.
