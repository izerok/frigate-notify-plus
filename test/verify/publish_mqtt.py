#!/usr/bin/env python3
"""Publish the two MQTT messages for the walk-through:
1. frigate/reviews type=new  -> initial alert
2. frigate/reviews type=genai -> GenAI update (caption edit path)
"""
import json
import sys
import time

import paho.mqtt.client as mqtt

REVIEW = {
    "id": "rev-verify-1",
    "camera": "front_door",
    "start_time": 1763790000.0,
    "end_time": 1763790010.0,
    "severity": "alert",
    "thumb_path": "",
    "data": {
        "detections": ["evt-verify-1"],
        "objects": ["person"],
        "sub_labels": [],
        "zones": [],
        "audio": [],
    },
}

GENAI = dict(REVIEW)
GENAI["data"] = dict(REVIEW["data"])
GENAI["data"]["metadata"] = {
    "title": "有人在门口",
    "shortSummary": "门口站着一个人，正在按门铃。",
    "scene": "前门",
    "confidence": 0.9,
    "potential_threat_level": 1,
    "other_concerns": [],
    "observations": ["一个人站在门口"],
    "time": "15:30",
}

GENAI2 = dict(GENAI)
GENAI2["data"] = dict(GENAI["data"])
GENAI2["data"]["metadata"] = dict(GENAI["data"]["metadata"])
GENAI2["data"]["metadata"].update({
    "title": "有人在撬门",
    "shortSummary": "门口站着一个人，正在撬门。",
    "potential_threat_level": 2,
    "observations": ["一个人站在门口，正在撬门"],
})

client = mqtt.Client(mqtt.CallbackAPIVersion.VERSION2)
client.connect("127.0.0.1", 1883, 60)
client.loop_start()

which = sys.argv[1]
if which == "new":
    client.publish("frigate/reviews", json.dumps({"type": "new", "after": REVIEW}))
elif which == "genai":
    client.publish("frigate/reviews", json.dumps({"type": "genai", "after": GENAI}))
elif which == "genai2":
    # GenAI update with threat level 2 (Critical) for the same review:
    # the vehicle for the phase 5 escalation check.
    client.publish("frigate/reviews", json.dumps({"type": "genai", "after": GENAI2}))
else:
    raise SystemExit(f"unknown message: {which}")
time.sleep(1)
client.loop_stop()
client.disconnect()
print(f"published {which}")
