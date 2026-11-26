"""Pure translation helpers for the Gemini Image Adapter.

No network, no I/O, no secrets. Fully unit-testable.
"""

from __future__ import annotations

import base64
import re
from typing import Any


class AdapterError(Exception):
    """Raised when a request or response cannot be handled."""

    def __init__(self, status: int, message: str) -> None:
        super().__init__(message)
        self.status = status
        self.message = message


_DATA_URL_RE = re.compile(r"^data:([^;]+);base64,(.+)$", re.IGNORECASE | re.DOTALL)

# Gemini Web2API returns the image embedded in markdown inside message.content:
#   ![image](data:image/png;base64,iVBORw0KGgo...)
_MARKDOWN_IMAGE_RE = re.compile(
    r"!\[[^\]]*\]\(\s*data:([^;]+);base64,([A-Za-z0-9+/=\s]+?)\s*\)",
    re.IGNORECASE | re.DOTALL,
)


def parse_data_url(url: str) -> bytes:
    """Decode a ``data:...;base64,...`` URL into raw bytes.

    Raises AdapterError(400) on malformed input.
    """
    if not isinstance(url, str) or not url:
        raise AdapterError(400, "image data URL is empty")
    m = _DATA_URL_RE.match(url.strip())
    if not m:
        raise AdapterError(400, "image data URL is not a base64 data URL")
    try:
        return base64.b64decode(m.group(2), validate=True)
    except Exception as exc:  # noqa: BLE001
        raise AdapterError(400, "invalid base64 in image data URL") from exc


def build_chat_request(prompt: str, model: str | None = None) -> dict[str, Any]:
    """Build the upstream chat completions request body."""
    if not isinstance(prompt, str) or not prompt.strip():
        raise AdapterError(400, "prompt is required")
    return {
        "model": model or "gemini-image",
        "messages": [{"role": "user", "content": prompt}],
        "stream": False,
    }


def extract_image_b64(chat_response: Any) -> str:
    """Extract a single base64 (raw, no data-URL prefix) image from a chat response.

    Gemini Web2API returns the image embedded in markdown inside message.content:
      ![image](data:image/png;base64,iVBORw0KGgo...)

    Raises AdapterError on any failure (no choices, no images, bad data URL).
    """
    if not isinstance(chat_response, dict):
        raise AdapterError(502, "upstream response is not JSON object")
    choices = chat_response.get("choices")
    if not isinstance(choices, list) or not choices:
        raise AdapterError(502, "upstream response has no choices")
    message = choices[0].get("message") if isinstance(choices[0], dict) else None
    if not isinstance(message, dict):
        raise AdapterError(502, "upstream response has no message")
    content = message.get("content")
    if not isinstance(content, str) or not content.strip():
        raise AdapterError(502, "upstream response has no content")
    m = _MARKDOWN_IMAGE_RE.search(content)
    if not m:
        raise AdapterError(502, "upstream response contains no image data URL")
    mime = m.group(1).strip().lower()
    if not mime.startswith("image/"):
        raise AdapterError(502, "upstream image has unsupported mime type: " + mime)
    b64 = m.group(2)
    # strip whitespace/newlines that may appear in base64
    b64 = "".join(b64.split())
    url = "data:" + mime + ";base64," + b64
    raw = parse_data_url(url)
    if not raw:
        raise AdapterError(502, "upstream image decoded to empty bytes")
    # magic bytes check for common image types
    if mime == "image/png" and not raw.startswith(b"\x89PNG\r\n\x1a\n"):
        raise AdapterError(502, "upstream data is not a valid PNG")
    if mime in ("image/jpeg", "image/jpg") and not raw.startswith(b"\xff\xd8\xff"):
        raise AdapterError(502, "upstream data is not a valid JPEG")
    return base64.b64encode(raw).decode("ascii")


def build_images_response(b64: str) -> dict[str, Any]:
    """Build an OpenAI Images API compatible response."""
    if not isinstance(b64, str) or not b64:
        raise AdapterError(500, "internal error: empty image payload")
    return {"data": [{"b64_json": b64}]}


def validate_request_body(body: Any) -> tuple[str, int]:
    """Validate and normalize an incoming images/generations request body.

    Returns (prompt, n). Raises AdapterError on invalid input.
    """
    if not isinstance(body, dict):
        raise AdapterError(400, "request body must be a JSON object")
    prompt = body.get("prompt")
    if not isinstance(prompt, str) or not prompt.strip():
        raise AdapterError(400, "prompt is required")
    n = body.get("n", 1)
    if isinstance(n, bool) or not isinstance(n, int):
        try:
            n = int(n)
        except (TypeError, ValueError) as exc:
            raise AdapterError(400, "n must be an integer") from exc
    if n < 1:
        raise AdapterError(400, "n must be >= 1")
    if n != 1:
        raise AdapterError(400, "n > 1 is not supported by this adapter (single image per request)")
    # response_format: only b64_json is produced; accept and ignore url/other.
    rf = body.get("response_format")
    if rf is not None and str(rf).lower() not in ("b64_json", "url"):
        raise AdapterError(400, "unsupported response_format")
    return prompt.strip(), n
