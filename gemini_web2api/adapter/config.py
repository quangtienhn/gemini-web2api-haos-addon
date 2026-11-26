"""Configuration loaded from environment variables."""

from __future__ import annotations

import os


def env(name: str, default: str = "") -> str:
    return os.environ.get(name, default).strip()


ADAPTER_HOST = env("ADAPTER_HOST", "127.0.0.1")
ADAPTER_PORT = int(env("ADAPTER_PORT", "8099") or "8099")
ADAPTER_API_KEY = env("ADAPTER_API_KEY", "")
ALLOW_UNAUTHENTICATED = env("ALLOW_UNAUTHENTICATED", "0") == "1"

UPSTREAM_BASE_URL = env("UPSTREAM_BASE_URL", "http://127.0.0.1:8083/v1").rstrip("/")
UPSTREAM_API_KEY = env("UPSTREAM_API_KEY", "")
UPSTREAM_MODEL = env("UPSTREAM_MODEL", "gemini-image")
UPSTREAM_TIMEOUT = int(env("UPSTREAM_TIMEOUT", "120") or "120")

MAX_REQUEST_BYTES = int(env("MAX_REQUEST_BYTES", "1048576") or "1048576")
MAX_RESPONSE_BYTES = int(env("MAX_RESPONSE_BYTES", "10485760") or "10485760")
LOG_LEVEL = env("LOG_LEVEL", "INFO").upper()
