#!/usr/bin/env python3
"""Minimal mock Frigate HTTP API for end-to-end verification.

Serves:
  GET /api/version                 -> "0.18.2"
  GET /api/config                  -> Frigate config (mqtt + cameras/zones)
  GET /api/events                  -> event list (sub_label values)
  GET /api/events/<id>             -> detection JSON
  GET /api/events/<id>/snapshot.jpg -> 1x1 JPEG bytes
Anything else -> 404.
"""
import base64
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# 1x1 white JPEG
JPEG_1X1 = base64.b64decode(
    "/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAP//////////////////////////////////////////////////////////////////////////////////////"
    "2wBDAf//////////////////////////////////////////////////////////////////////////////////////wAARCAABAAEDASIAAhEBAxEB/8QA"
    "FwAAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoL/8QAtRAAAgEDAwIEAwUFBAQAAAF9AQIDAAQRBRIh"
    "MUEGE1FhByJxFDKBkaEII0KxwRVS0fAkM2JyggkKFhcYGRolJicoKSo0NTY3ODk6Q0RFRkdISUpTVFVW"
    "Vl5iZmqSlZGVmZ2hpanN0dXZ3eHl6hoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfI"
    "ycrS09TV1tfY2dri4+Tl5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/AJVUUBQf/2Q=="
)

EVENT = {
    "id": "evt-verify-1",
    "camera": "front_door",
    "label": "person",
    "sub_label": None,
    "top_score": 0.92,
    "false_positive": False,
    "start_time": 1763790000.0,
    "end_time": 1763790010.0,
    "zones": [],
    "data": {"top_score": 0.92, "type": "object"},
    "has_clip": True,
    "has_snapshot": True,
}

# Small /api/config payload: mqtt section + two cameras with zones.
# The mqtt password is deliberately the Frigate 0.18 redaction sentinel:
# a real Frigate never returns the password verbatim, and frigate-notify-plus
# must not echo it back through /api/v1/frigate_meta.
CONFIG = {
    "cameras": {
        "front_door": {"zones": {"yard": {}, "driveway": {}}},
        "back_yard": {"zones": {"patio": {}}},
    },
    "objects": {"track": ["person", "car", "dog"]},
    "model": {"labelmap": {}},
    "mqtt": {
        "enabled": True,
        "host": "127.0.0.1",
        "port": 1883,
        "user": "frigate",
        "password": "__FRIGATE_SAVED_CREDENTIAL__",
    },
}

# /api/events list payload: duplicate + empty sub_labels exercise the
# dedup / filter logic in /api/v1/frigate_meta.
EVENTS = [
    {"id": "evt-verify-1", "camera": "front_door", "label": "person", "sub_label": "delivery"},
    {"id": "evt-verify-2", "camera": "front_door", "label": "person", "sub_label": "delivery"},
    {"id": "evt-verify-3", "camera": "back_yard", "label": "person", "sub_label": "mailman"},
    {"id": "evt-verify-4", "camera": "back_yard", "label": "dog", "sub_label": None},
]


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_POST(self):
        # Frigate GenAI review summary: POST
        # /api/review/summarize/start/<unix>/end/<unix> -> {"success": true, "summary": ...}
        path = self.path.split("?")[0]
        if path.startswith("/api/review/summarize/start/") and "/end/" in path:
            self._send(200, "application/json", json.dumps({
                "success": True,
                "summary": "定时总结测试：白天一切正常，无可疑活动。",
            }).encode())
        else:
            self._send(404, "text/plain", b"not found")

    def do_GET(self):
        path = self.path.split("?")[0]
        if path == "/api/version":
            self._send(200, "text/plain", b"0.18.2")
        elif path == "/api/config":
            self._send(200, "application/json", json.dumps(CONFIG).encode())
        elif path == "/api/events":
            self._send(200, "application/json", json.dumps(EVENTS).encode())
        elif path.startswith("/api/events/") and path.endswith("/snapshot.jpg"):
            self._send(200, "image/jpeg", JPEG_1X1)
        elif path.startswith("/api/events/"):
            body = json.dumps(EVENT).encode()
            self._send(200, "application/json", body)
        else:
            self._send(404, "text/plain", b"not found")

    def _send(self, code, ctype, body):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", 18080), Handler).serve_forever()
