#!/usr/bin/env python3
"""Webhook catcher: records every POST body as one JSON line.
Routes by path so channels stay countable separately:
  /bark  -> /tmp/bark-posts.jsonl  (answers Bark's {"code":200,...} JSON)
  /hook2 -> /tmp/webhook2-posts.jsonl
  anything else (/hook) -> /tmp/webhook-posts.jsonl
"""
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LOGS = {
    "/tmp/webhook-posts.jsonl": ("/hook",),
    "/tmp/webhook2-posts.jsonl": ("/hook2",),
    "/tmp/bark-posts.jsonl": ("/bark",),
}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)
        # /hook2 must be checked before /hook: it is a prefix of it.
        if self.path.startswith("/bark"):
            log = "/tmp/bark-posts.jsonl"
            reply = b'{"code":200,"message":"success"}'
            ctype = "application/json"
        elif self.path.startswith("/hook2"):
            log = "/tmp/webhook2-posts.jsonl"
            reply = b"ok"
            ctype = "text/plain"
        else:
            log = "/tmp/webhook-posts.jsonl"
            reply = b"ok"
            ctype = "text/plain"
        with open(log, "a") as f:
            f.write(body.decode("utf-8", "replace") + "\n")
        self.send_response(200)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(reply)))
        self.end_headers()
        self.wfile.write(reply)


if __name__ == "__main__":
    for log in LOGS:
        open(log, "w").close()
    ThreadingHTTPServer(("127.0.0.1", 18081), Handler).serve_forever()
