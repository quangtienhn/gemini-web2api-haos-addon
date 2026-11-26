"""Unit tests for pure adapter helpers."""

from __future__ import annotations

import base64
import json

import adapter


def make_data_url(raw: bytes, mime: str = "image/jpeg") -> str:
    return "data:%s;base64,%s" % (mime, base64.b64encode(raw).decode("ascii"))


def test_parse_data_url_roundtrip():
    raw = b"\xff\xd8\xff" + b"abc" * 10
    url = make_data_url(raw)
    assert adapter.parse_data_url(url) == raw


def test_parse_data_url_rejects_garbage():
    try:
        adapter.parse_data_url("not-a-data-url")
    except adapter.AdapterError as exc:
        assert exc.status == 400
    else:
        raise AssertionError("expected AdapterError")


def test_parse_data_url_rejects_empty():
    try:
        adapter.parse_data_url("")
    except adapter.AdapterError as exc:
        assert exc.status == 400
    else:
        raise AssertionError("expected AdapterError")


def test_build_chat_request():
    r = adapter.build_chat_request("draw a cat")
    assert r["model"] == "gemini-image"
    assert r["messages"][0]["content"] == "draw a cat"
    assert r["stream"] is False


def test_build_chat_request_rejects_empty():
    try:
        adapter.build_chat_request("   ")
    except adapter.AdapterError as exc:
        assert exc.status == 400
    else:
        raise AssertionError("expected AdapterError")


def test_extract_image_b64_success():
    raw = b"\x89PNG\r\n\x1a\n" + b"hello-image-bytes"
    data_url = "data:image/png;base64,%s" % base64.b64encode(raw).decode("ascii")
    resp = {
        "choices": [{
            "message": {
                "content": "Here is your image: ![image](%s)" % data_url,
            }
        }]
    }
    b64 = adapter.extract_image_b64(resp)
    assert base64.b64decode(b64) == raw


def test_extract_image_b64_no_images():
    resp = {"choices": [{"message": {"content": "no image"}}]}
    try:
        adapter.extract_image_b64(resp)
    except adapter.AdapterError as exc:
        assert exc.status == 502
    else:
        raise AssertionError("expected AdapterError")


def test_extract_image_b64_no_choices():
    try:
        adapter.extract_image_b64({})
    except adapter.AdapterError as exc:
        assert exc.status == 502
    else:
        raise AssertionError("expected AdapterError")


def test_extract_image_b64_bad_data_url():
    # Non-data-URL markdown image -> not a supported image source.
    resp = {"choices": [{"message": {"content": "![x](https://example.com/x.png)"}}]}
    try:
        adapter.extract_image_b64(resp)
    except adapter.AdapterError as exc:
        assert exc.status == 502
    else:
        raise AssertionError("expected AdapterError")


def test_build_images_response():
    r = adapter.build_images_response("AAAA")
    assert r == {"data": [{"b64_json": "AAAA"}]}


def test_validate_request_body_ok():
    prompt, n = adapter.validate_request_body({"prompt": "hi", "n": 1, "response_format": "b64_json"})
    assert prompt == "hi" and n == 1


def test_validate_request_body_default_n():
    prompt, n = adapter.validate_request_body({"prompt": "hi"})
    assert n == 1


def test_validate_request_body_rejects_n_gt_1():
    try:
        adapter.validate_request_body({"prompt": "hi", "n": 2})
    except adapter.AdapterError as exc:
        assert exc.status == 400
    else:
        raise AssertionError("expected AdapterError")


def test_validate_request_body_rejects_empty_prompt():
    try:
        adapter.validate_request_body({"prompt": "  "})
    except adapter.AdapterError as exc:
        assert exc.status == 400
    else:
        raise AssertionError("expected AdapterError")


def test_validate_request_body_rejects_bad_response_format():
    try:
        adapter.validate_request_body({"prompt": "hi", "response_format": "xml"})
    except adapter.AdapterError as exc:
        assert exc.status == 400
    else:
        raise AssertionError("expected AdapterError")


def test_validate_request_body_rejects_non_object():
    try:
        adapter.validate_request_body(["x"])
    except adapter.AdapterError as exc:
        assert exc.status == 400
    else:
        raise AssertionError("expected AdapterError")


def test_build_chat_request_uses_config_model():
    r = adapter.build_chat_request("draw a cat", model="gemini-image")
    assert r["model"] == "gemini-image"
    assert r["messages"][0]["content"] == "draw a cat"
    assert r["stream"] is False


def test_extract_image_b64_rejects_invalid_base64():
    # Base64 with invalid characters should be rejected (strict validation).
    bad = "data:image/png;base64,!!!notbase64!!!"
    resp = {"choices": [{"message": {"content": "![](%s)" % bad}}]}
    try:
        adapter.extract_image_b64(resp)
    except adapter.AdapterError as exc:
        assert exc.status == 502
    else:
        raise AssertionError("expected AdapterError")
