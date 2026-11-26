# Gemini Web2API Add-ons

Home Assistant app repository for Gemini Web2API.

## Repository URL

```
https://github.com/quangtienhn/gemini-web2api-haos-addon
```

## Install in Home Assistant

1. Settings → Apps → Install app.
2. Menu (⋮) → Repositories.
3. Add the repository URL above.
4. Find **Gemini Web2API** in the repository and install it.

The app pulls its multi-architecture image from GHCR; no Docker build is required on the HAOS host.

## Documentation

See `gemini_web2api/DOCS.md` and `gemini_web2api/README_VI.md`.

## Security notes

- Configure `admin_token` (at least 32 characters) before starting.
- Anonymous mode, URL fetching, account pool and proxy pool are disabled by default.
- Do not expose TCP port 8083 to the public internet.
