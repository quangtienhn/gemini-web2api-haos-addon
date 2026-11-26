"""HTTP-level tests for the adapter server with a mocked upstream."""

from __future__ import annotations

import base64
import json
import os
import threading
import time
import unittest
import urllib.error
import urllib.request

# Ensure module import works when running from repo root.
HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
if ROOT not in os.sys.path:
    os.sys.path.insert(0, ROOT)

import adapter  # noqa: E402
import app  # noqa: E402
import config  # noqa: E402


def make_data_url(raw: bytes) -> str:
    return "data:image/jpeg;base64," + base64.b64encode(raw).decode("ascii")


class _ServerThread(threading.Thread):
    def __init__(self, server):
        super().__init__(daemon=True)
        self.server = server

    def run(self):
        self.server.serve_forever()

    def stop(self):
        self.server.shutdown()


class TestServer(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # Configure adapter for unauthenticated local testing.
        config.ADAPTER_API_KEY = ""
        config.ALLOW_UNAUTHENTICATED = True
        config.UPSTREAM_BASE_URL = "http://127.0.0.1:9"  # unused; upstream mocked
        config.ADAPTER_HOST = "127.0.0.1"
        config.ADAPTER_PORT = 0
        cls.httpd = app.ThreadingHTTPServer((config.ADAPTER_HOST, config.ADAPTER_PORT), app.Handler)
        cls.port = cls.httpd.server_address[1]
        cls.thread = _ServerThread(cls.httpd)
        cls.thread.start()
        time.sleep(0.2)

    @classmethod
    def tearDownClass(cls):
        cls.thread.stop()
        cls.httpd.server_close()

    def _url(self, path):
        return "http://127.0.0.1:%d%s" % (self.port, path)

    def test_health(self):
        with urllib.request.urlopen(self._url("/health")) as r:
            assert r.status == 200
            data = json.loads(r.read())
            assert data["status"] == "ok"

    def test_models_requires_auth_when_key_set(self):
        # Temporarily require a key.
        old_key = config.ADAPTER_API_KEY
        old_allow = config.ALLOW_UNAUTHENTICATED
        config.ADAPTER_API_KEY = "secret"
        config.ALLOW_UNAUTHENTICATED = False
        try:
            req = urllib.request.Request(self._url("/v1/models"))
            try:
                urllib.request.urlopen(req)
            except urllib.error.HTTPError as e:
                assert e.code == 401
            else:
                raise AssertionError("expected 401")
        finally:
            config.ADAPTER_API_KEY = old_key
            config.ALLOW_UNAUTHENTICATED = old_allow

    def test_models_with_key(self):
        old_key = config.ADAPTER_API_KEY
        old_allow = config.ALLOW_UNAUTHENTICATED
        config.ADAPTER_API_KEY = "secret"
        config.ALLOW_UNAUTHENTICATED = False
        try:
            req = urllib.request.Request(self._url("/v1/models"), headers={"Authorization": "Bearer secret"})
            with urllib.request.urlopen(req) as r:
                data = json.loads(r.read())
                assert data["object"] == "list"
                assert data["data"][0]["id"] == config.UPSTREAM_MODEL
        finally:
            config.ADAPTER_API_KEY = old_key
            config.ALLOW_UNAUTHENTICATED = old_allow

    def test_generate_success_mocked(self):
        raw = b"\xff\xd8\xff" + b"x" * 100
        chat_resp = {"choices": [{"message": {"content": "![image](%s)" % make_data_url(raw)}}]}

        original = app.call_upstream_chat
        app.call_upstream_chat = lambda prompt: chat_resp
        try:
            data = {"prompt": "draw a square", "n": 1, "response_format": "b64_json"}
            req = urllib.request.Request(
                self._url("/v1/images/generations"),
                data=json.dumps(data).encode(),
                headers={"Content-Type": "application/json"},
                method="POST",
            )
            with urllib.request.urlopen(req) as r:
                assert r.status == 200
                out = json.loads(r.read())
                assert "data" in out and len(out["data"]) == 1
                b64 = out["data"][0]["b64_json"]
                assert base64.b64decode(b64) == raw
        finally:
            app.call_upstream_chat = original

    def test_generate_upstream_no_image(self):
        original = app.call_upstream_chat
        app.call_upstream_chat = lambda prompt: {"choices": [{"message": {"content": "no image"}}]}
        try:
            data = {"prompt": "draw a square"}
            req = urllib.request.Request(
                self._url("/v1/images/generations"),
                data=json.dumps(data).encode(),
                headers={"Content-Type": "application/json"},
                method="POST",
            )
            try:
                urllib.request.urlopen(req)
            except urllib.error.HTTPError as e:
                assert e.code == 502
            else:
                raise AssertionError("expected 502")
        finally:
            app.call_upstream_chat = original

    def test_generate_upstream_error_propagates(self):
        def boom(prompt):
            raise adapter.AdapterError(429, "upstream: rate limited")
        original = app.call_upstream_chat
        app.call_upstream_chat = boom
        try:
            data = {"prompt": "draw a square"}
            req = urllib.request.Request(
                self._url("/v1/images/generations"),
                data=json.dumps(data).encode(),
                headers={"Content-Type": "application/json"},
                method="POST",
            )
            try:
                urllib.request.urlopen(req)
            except urllib.error.HTTPError as e:
                assert e.code == 429
            else:
                raise AssertionError("expected 429")
        finally:
            app.call_upstream_chat = original

    def test_generate_missing_prompt(self):
        data = {"prompt": "   "}
        req = urllib.request.Request(
            self._url("/v1/images/generations"),
            data=json.dumps(data).encode(),
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        try:
            urllib.request.urlopen(req)
        except urllib.error.HTTPError as e:
            assert e.code == 400
        else:
            raise AssertionError("expected 400")

    def test_generate_n_gt_1(self):
        data = {"prompt": "draw", "n": 3}
        req = urllib.request.Request(
            self._url("/v1/images/generations"),
            data=json.dumps(data).encode(),
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        try:
            urllib.request.urlopen(req)
        except urllib.error.HTTPError as e:
            assert e.code == 400
        else:
            raise AssertionError("expected 400")

    def test_unknown_path_404(self):
        req = urllib.request.Request(self._url("/nope"), method="GET")
        try:
            urllib.request.urlopen(req)
        except urllib.error.HTTPError as e:
            assert e.code == 404
        else:
            raise AssertionError("expected 404")


if __name__ == "__main__":
    unittest.main()
