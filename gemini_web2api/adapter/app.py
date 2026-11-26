"""HTTP server for the Gemini Image Adapter.

Stdlib only (no third-party runtime dependencies).
"""

from __future__ import annotations

import base64
import json
import logging
import os
import signal
import sys
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

import adapter
import config

logger = logging.getLogger("gemini-image-adapter")

ADAPTER_VERSION = "0.1.0"


def _json_response(handler: BaseHTTPRequestHandler, status: int, payload: Any) -> None:
    body = json.dumps(payload).encode("utf-8")
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json")
    handler.send_header("Content-Length", str(len(body)))
    handler.end_headers()
    handler.wfile.write(body)


def _error(handler: BaseHTTPRequestHandler, status: int, message: str) -> None:
    _json_response(handler, status, {"error": {"message": message, "type": "invalid_request_error" if status < 500 else "server_error"}})


def _read_body(handler: BaseHTTPRequestHandler) -> bytes:
    length = handler.headers.get("Content-Length")
    if length is None:
        raise adapter.AdapterError(411, "Content-Length is required")
    try:
        n = int(length)
    except ValueError as exc:
        raise adapter.AdapterError(400, "invalid Content-Length") from exc
    if n < 0 or n > config.MAX_REQUEST_BYTES:
        raise adapter.AdapterError(413, "request body too large")
    data = handler.rfile.read(n) if n > 0 else b""
    if len(data) != n:
        raise adapter.AdapterError(400, "short read on request body")
    return data


def _check_auth(handler: BaseHTTPRequestHandler) -> bool:
    if config.ALLOW_UNAUTHENTICATED:
        return True
    if not config.ADAPTER_API_KEY:
        _error(handler, 500, "adapter is not configured with ADAPTER_API_KEY")
        return False
    auth = handler.headers.get("Authorization", "")
    expected = "Bearer " + config.ADAPTER_API_KEY
    if auth != expected:
        _error(handler, 401, "invalid or missing API key")
        return False
    return True


def call_upstream_chat(prompt: str) -> dict[str, Any]:
    """Call CLIProxyAPI /v1/chat/completions and return parsed JSON."""
    body = adapter.build_chat_request(prompt, model=config.UPSTREAM_MODEL)
    data = json.dumps(body).encode("utf-8")
    url = config.UPSTREAM_BASE_URL + "/chat/completions"
    req = urllib.request.Request(url, data=data, method="POST")
    req.add_header("Content-Type", "application/json")
    if config.UPSTREAM_API_KEY:
        req.add_header("Authorization", "Bearer " + config.UPSTREAM_API_KEY)
    req.add_header("Accept", "application/json")
    t0 = time.time()
    try:
        with urllib.request.urlopen(req, timeout=config.UPSTREAM_TIMEOUT) as resp:
            raw = resp.read(config.MAX_RESPONSE_BYTES + 1)
            status = resp.status
    except urllib.error.HTTPError as exc:
        raw = exc.read(config.MAX_RESPONSE_BYTES + 1)
        status = exc.code
        # Re-raise as AdapterError with upstream detail (sanitized).
        detail = raw.decode("utf-8", "replace")
        try:
            j = json.loads(detail)
            msg = j.get("error", {}).get("message") if isinstance(j.get("error"), dict) else None
            if not msg:
                msg = j.get("message")
        except Exception:  # noqa: BLE001
            msg = None
        if not msg:
            msg = "upstream error"
        raise adapter.AdapterError(status if 400 <= status < 600 else 502, "upstream: " + str(msg)[:300])
    except Exception as exc:  # noqa: BLE001
        raise adapter.AdapterError(504, "upstream request failed: " + str(exc)[:200])

    if len(raw) > config.MAX_RESPONSE_BYTES:
        raise adapter.AdapterError(502, "upstream response too large")
    try:
        parsed = json.loads(raw.decode("utf-8"))
    except Exception as exc:  # noqa: BLE001
        raise adapter.AdapterError(502, "upstream response is not JSON") from exc
    elapsed = time.time() - t0
    logger.info("upstream chat status=%s elapsed=%.2fs", status, elapsed)
    if status < 200 or status >= 300:
        raise adapter.AdapterError(502, "upstream returned HTTP %d" % status)
    return parsed


class Handler(BaseHTTPRequestHandler):
    server_version = "gemini-image-adapter/" + ADAPTER_VERSION

    def log_message(self, fmt, *args):  # noqa: A003
        # Avoid logging request bodies/headers; basic access log only.
        logger.info("%s - %s", self.address_string(), fmt % args)

    def do_GET(self):  # noqa: N802
        if self.path.split("?")[0] == "/health":
            _json_response(self, 200, {"status": "ok", "version": ADAPTER_VERSION})
            return
        if self.path.split("?")[0] == "/v1/models":
            if not _check_auth(self):
                return
            _json_response(self, 200, {
                "object": "list",
                "data": [{
                    "id": config.UPSTREAM_MODEL,
                    "object": "model",
                    "created": int(time.time()),
                    "owned_by": "google",
                }],
            })
            return
        _error(self, 404, "not found")

    def do_POST(self):  # noqa: N802
        path = self.path.split("?")[0]
        if path != "/v1/images/generations":
            _error(self, 404, "not found")
            return
        if not _check_auth(self):
            return
        t0 = time.time()
        try:
            raw = _read_body(self)
        except adapter.AdapterError as exc:
            _error(self, exc.status, exc.message)
            return
        try:
            body = json.loads(raw.decode("utf-8")) if raw else {}
        except Exception:  # noqa: BLE001
            _error(self, 400, "invalid JSON body")
            return
        try:
            prompt, n = adapter.validate_request_body(body)
        except adapter.AdapterError as exc:
            _error(self, exc.status, exc.message)
            return

        try:
            chat_resp = call_upstream_chat(prompt)
            b64 = adapter.extract_image_b64(chat_resp)
            out = adapter.build_images_response(b64)
        except adapter.AdapterError as exc:
            logger.warning("request failed: status=%s msg=%s", exc.status, exc.message)
            _error(self, exc.status, exc.message)
            return
        except Exception as exc:  # noqa: BLE001
            logger.exception("unexpected error")
            _error(self, 500, "internal error: " + str(exc)[:200])
            return

        elapsed = time.time() - t0
        # Decode to verify size without logging content.
        try:
            img_bytes = base64.b64decode(out["data"][0]["b64_json"], validate=True)
            img_len = len(img_bytes)
        except Exception:  # noqa: BLE001
            img_len = -1
        logger.info("image generated elapsed=%.2fs bytes=%d", elapsed, img_len)
        _json_response(self, 200, out)


class GracefulHTTPServer(ThreadingHTTPServer):
    daemon_threads = True
    allow_reuse_address = True


def main() -> int:
    logging.basicConfig(
        level=getattr(logging, config.LOG_LEVEL, logging.INFO),
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    )
    if not config.ADAPTER_API_KEY and not config.ALLOW_UNAUTHENTICATED:
        logger.error("ADAPTER_API_KEY is required (or set ALLOW_UNAUTHENTICATED=1 for local testing only)")
        return 2
    if not config.UPSTREAM_API_KEY:
        logger.warning("UPSTREAM_API_KEY is empty; upstream may reject requests")

    httpd = GracefulHTTPServer((config.ADAPTER_HOST, config.ADAPTER_PORT), Handler)
    shutdown_event = threading.Event()

    def _handle_signal(signum, _frame):
        if shutdown_event.is_set():
            return
        shutdown_event.set()
        logger.info("received signal %s, shutting down", signum)
        # shutdown() must not be called from the serve_forever thread.
        threading.Thread(target=httpd.shutdown, daemon=True).start()

    signal.signal(signal.SIGTERM, _handle_signal)
    signal.signal(signal.SIGINT, _handle_signal)

    logger.info("listening on %s:%d", config.ADAPTER_HOST, config.ADAPTER_PORT)
    logger.info("upstream=%s model=%s", config.UPSTREAM_BASE_URL, config.UPSTREAM_MODEL)
    try:
        httpd.serve_forever(poll_interval=0.5)
    finally:
        try:
            httpd.server_close()
        except Exception:  # noqa: BLE001
            logger.exception("error closing server")
    logger.info("server stopped")
    return 0


if __name__ == "__main__":
    sys.exit(main())
