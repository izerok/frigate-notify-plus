#!/usr/bin/env python3
"""Tiny HTTP forward proxy for the verify run.

Listens on 127.0.0.1:18082. Accepts plain-HTTP proxy requests
(absolute-URI form), forwards them to the target with http.client, and
appends one line per forwarded request ("METHOD absolute-uri") to
/tmp/proxy-hits.log so check.sh can prove a delivery transited the
proxy. Non-proxy requests (origin-form) are rejected with 400 and are
not logged.
"""

import http.client
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit

LOG_PATH = "/tmp/proxy-hits.log"
_log_lock = threading.Lock()


def log_hit(method: str, uri: str) -> None:
    with _log_lock:
        with open(LOG_PATH, "a", encoding="utf-8") as fh:
            fh.write(f"{method} {uri}\n")
            fh.flush()


class ForwardProxyHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):  # keep the workflow log quiet
        pass

    def _forward(self) -> None:
        target = urlsplit(self.path)
        if target.scheme not in ("http", "https") or not target.hostname:
            self.send_error(400, "expected an absolute-URI proxy request")
            return

        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length else None

        headers = {
            k: v
            for k, v in self.headers.items()
            if k.lower()
            not in ("proxy-authorization", "proxy-connection", "connection", "host")
        }
        path = target.path or "/"
        if target.query:
            path += "?" + target.query

        log_hit(self.command, self.path)
        conn_cls = http.client.HTTPSConnection if target.scheme == "https" else http.client.HTTPConnection
        conn = conn_cls(target.hostname, target.port, timeout=30)
        try:
            conn.request(self.command, path, body=body, headers=headers)
            resp = conn.getresponse()
            resp_body = resp.read()
        except Exception as exc:  # target unreachable: report like a real proxy
            self.send_error(502, f"proxy upstream error: {exc}")
            return
        finally:
            conn.close()

        self.send_response(resp.status, resp.reason)
        for k, v in resp.getheaders():
            if k.lower() in ("transfer-encoding", "connection"):
                continue
            self.send_header(k, v)
        self.send_header("Content-Length", str(len(resp_body)))
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(resp_body)

    do_GET = _forward
    do_POST = _forward
    do_PUT = _forward
    do_DELETE = _forward


if __name__ == "__main__":
    open(LOG_PATH, "w").close()
    ThreadingHTTPServer(("127.0.0.1", 18082), ForwardProxyHandler).serve_forever()
