#!/usr/bin/env bash
# End-to-end assertions for the Web UI + GenAI walk-through.
# Expects: mock Frigate on :18080, webhook catcher on :18081,
# mosquitto on :1883, frigate-notify-plus container on :8000.
set -euo pipefail

API=http://127.0.0.1:8000

echo "== publish new review =="
python3 test/verify/publish_mqtt.py new

echo "== wait for 1st webhook post (up to 60s) =="
for _ in $(seq 1 30); do
  n=$(wc -l < /tmp/webhook-posts.jsonl 2>/dev/null || echo 0)
  [ "$n" -ge 1 ] && break
  sleep 2
done
[ "$(wc -l < /tmp/webhook-posts.jsonl)" -ge 1 ] || { echo "FAIL: no webhook post for new review"; exit 1; }

echo "== publish genai update =="
python3 test/verify/publish_mqtt.py genai

echo "== wait for 2nd webhook post (up to 60s) =="
for _ in $(seq 1 30); do
  n=$(wc -l < /tmp/webhook-posts.jsonl 2>/dev/null || echo 0)
  [ "$n" -ge 2 ] && break
  sleep 2
done
[ "$(wc -l < /tmp/webhook-posts.jsonl)" -ge 2 ] || { echo "FAIL: no webhook post for genai update"; exit 1; }

python3 - <<'EOF'
import json
posts = [json.loads(l) for l in open('/tmp/webhook-posts.jsonl') if l.strip()]
assert len(posts) >= 2, posts
second = json.dumps(posts[1], ensure_ascii=False)
assert "有人在门口" in second, f"GenAI title missing in 2nd post: {second[:500]}"
assert "按门铃" in second, f"GenAI summary missing in 2nd post: {second[:500]}"
assert posts[1].get("genai", {}).get("threat_level") == "Suspicious", second[:500]
print("webhook posts ok:", len(posts))
EOF

echo "== history: 2 entries, newest is the genai update =="
curl -fsS $API/api/v1/history > /tmp/history.json
python3 - <<'EOF'
import json
h = json.load(open('/tmp/history.json'))["history"]
assert len(h) == 2, h
assert h[0]["is_genai_update"] is True, h[0]
assert h[0]["genai_title"] == "有人在门口", h[0]
assert h[1].get("is_genai_update", False) is False, h[1]  # false bools are omitted by omitempty
assert h[0]["providers"] == ["webhook"], h[0]  # sender names are the lowercase config keys
print("history ok")
EOF

echo "== config round-trip via JSON API =="
curl -fsS $API/api/v1/config > /tmp/config-got.json
python3 - <<'EOF'
import json
c = json.load(open('/tmp/config-got.json'))["config"]
assert c["alerts"]["general"]["title"] == "Verify Alert", c["alerts"]["general"]
assert c["alerts"]["general"]["genai"]["enabled"] is True
assert c["alerts"]["webhook"][0]["server"] == "http://127.0.0.1:18081/hook"
c["alerts"]["general"]["title"] = "Verify Alert 2"
json.dump({"config": c}, open('/tmp/config-put.json', 'w'))
EOF
curl -fsS -X PUT -H 'Content-Type: application/json' --data @/tmp/config-put.json $API/api/v1/config | grep -q '"status":"ok"'
sleep 3
curl -fsS $API/api/v1/config | python3 -c "import json,sys; c=json.load(sys.stdin)['config']; assert c['alerts']['general']['title']=='Verify Alert 2', c['alerts']['general']; print('config PUT round-trip ok')"

echo "== config round-trip via YAML API =="
curl -fsS $API/api/v1/config/yaml | grep -q "server: http://127.0.0.1:18080"
curl -fsS $API/api/v1/config/yaml > /tmp/config.yml
sed -i 's/title: Verify Alert 2/title: Verify Alert 3/' /tmp/config.yml
curl -fsS -X PUT -H 'Content-Type: text/yaml' --data-binary @/tmp/config.yml $API/api/v1/config/yaml | grep -q '"status":"ok"'
sleep 3
curl -fsS $API/api/v1/config | python3 -c "import json,sys; c=json.load(sys.stdin)['config']; assert c['alerts']['general']['title']=='Verify Alert 3', c['alerts']['general']; print('yaml PUT round-trip ok')"

echo "== templates API =="
curl -fsS $API/api/v1/templates | python3 -c "import json,sys; d=json.load(sys.stdin); assert {'plaintext','markdown','html'} <= set(d), d; print('templates GET ok')"  # huma adds a \$schema key
curl -fsS $API/api/v1/templates > /tmp/templates.json
python3 - <<'EOF'
import json
d = {k: v for k, v in json.load(open('/tmp/templates.json')).items() if not k.startswith('$')}
d["markdown"] = d["markdown"] + "\nVERIFY-MARKER\n"
json.dump(d, open('/tmp/templates-put.json', 'w'))
EOF
curl -fsS -X PUT -H 'Content-Type: application/json' --data @/tmp/templates-put.json $API/api/v1/templates | grep -q '"status":"ok"'
curl -fsS $API/api/v1/templates | grep -q "VERIFY-MARKER" && echo "templates PUT round-trip ok"
if [ -n "${DATA_DIR:-}" ]; then
  test -f "$DATA_DIR/templates/markdown.template" || { echo "FAIL: no markdown.template in data dir"; exit 1; }
  grep -q "VERIFY-MARKER" "$DATA_DIR/templates/markdown.template" || { echo "FAIL: data-dir markdown.template lacks VERIFY-MARKER"; exit 1; }
  echo "templates data dir ok: $DATA_DIR/templates/markdown.template"
fi

echo "== UI page + status =="
curl -fsS $API/ | grep -q "最近通知" && echo "UI page ok"
curl -fsS $API/api/v1/status | python3 -c "import json,sys; d=json.load(sys.stdin)['status']; assert d['health']=='ok', d; print('status ok:', d['health'])"

echo "== phase 2: routing / mute / frigate_check / templates render =="
# Helpers for the phase 2 steps below.
wait_for_posts() {
  # wait_for_posts <want> <timeout_s>: 0 once the webhook catcher has >= want lines
  local want=$1 timeout=$2 i n
  for i in $(seq 1 "$timeout"); do
    n=$(wc -l < /tmp/webhook-posts.jsonl 2>/dev/null || echo 0)
    [ "$n" -ge "$want" ] && return 0
    sleep 1
  done
  return 1
}
publish_genai_expect() {
  # publish_genai_expect <want>: publish a genai update, waiting up to 30s
  # for the post count to reach <want>; publish once more if it does not.
  # NOTE: the steps below publish "genai", not "new": re-publishing "new"
  # for the same review is dropped by the zone cache (the mock Frigate
  # always returns evt-verify-1 with no new zones), while GenAI updates
  # bypass the zone cache by design and still flow through routing and
  # the mute gate, so they are the correct vehicle for these steps.
  local want=$1
  python3 test/verify/publish_mqtt.py genai
  if wait_for_posts "$want" 30; then return 0; fi
  echo "retrying genai publish (post $want not seen in 30s)"
  python3 test/verify/publish_mqtt.py genai
  wait_for_posts "$want" 30 || { echo "FAIL: no webhook post #$want for genai update"; exit 1; }
}

echo "== phase 2 (1): routing match =="
curl -fsS $API/api/v1/config > /tmp/config-got.json
python3 - <<'EOF'
import json
c = json.load(open('/tmp/config-got.json'))["config"]
c["alerts"].setdefault("routing", {})["rules"] = [
    {"name": "verify-route", "cameras": ["front_door"],
     "targets": [{"provider": "webhook", "index": 0}]}
]
json.dump({"config": c}, open('/tmp/config-put.json', 'w'))
EOF
curl -fsS -X PUT -H 'Content-Type: application/json' --data @/tmp/config-put.json $API/api/v1/config | grep -q '"status":"ok"'
sleep 3
publish_genai_expect 3
echo "routing match ok: 3rd webhook post arrived"

echo "== phase 2 (2): routing fallback =="
curl -fsS $API/api/v1/config > /tmp/config-got.json
python3 - <<'EOF'
import json
c = json.load(open('/tmp/config-got.json'))["config"]
c["alerts"].setdefault("routing", {})["rules"] = [
    {"name": "verify-route-nomatch", "cameras": ["other_cam"],
     "targets": [{"provider": "webhook", "index": 0}]}
]
json.dump({"config": c}, open('/tmp/config-put.json', 'w'))
EOF
curl -fsS -X PUT -H 'Content-Type: application/json' --data @/tmp/config-put.json $API/api/v1/config | grep -q '"status":"ok"'
sleep 3
publish_genai_expect 4
echo "routing fallback ok: 4th webhook post arrived"

echo "== phase 2 (3): per-camera mute =="
curl -fsS -X POST -H 'Content-Type: application/json' -d '{"camera":"front_door","minutes":30}' $API/api/v1/mute | grep -q '"status":"ok"'
python3 test/verify/publish_mqtt.py genai
sleep 12
n=$(wc -l < /tmp/webhook-posts.jsonl)
[ "$n" -eq 4 ] || { echo "FAIL: muted camera still posted (count $n, want 4)"; exit 1; }
curl -fsS $API/api/v1/history > /tmp/history.json
python3 - <<'EOF'
import json
h = json.load(open('/tmp/history.json'))["history"]
assert h, h
newest = h[0]
assert newest["ok"] is False, newest
assert "已静音" in newest.get("detail", ""), newest
print("muted drop history ok:", newest["detail"])
EOF
curl -fsS -X DELETE "$API/api/v1/mute?camera=front_door" | grep -q '"status":"ok"'
publish_genai_expect 5
echo "mute ok: 5th webhook post arrived after unmute"

echo "== phase 2 (4): frigate_check =="
curl -fsS -X POST -H 'Content-Type: application/json' -d '{"server":"http://127.0.0.1:18080"}' $API/api/v1/frigate_check | python3 -c "import json,sys; d=json.load(sys.stdin); assert d['ok'] is True and d['version']=='0.18.2' and d['major']==18 and d['error']=='', d; print('frigate_check ok:', d)"

echo "== phase 2 (5): templates/render =="
curl -fsS -X POST -H 'Content-Type: application/json' -d '{"name":"markdown"}' $API/api/v1/templates/render | python3 -c "import json,sys; d=json.load(sys.stdin); assert '有人在门口' in d['rendered'], d; print('templates/render ok')"
code=$(curl -s -o /tmp/render-bad.json -w '%{http_code}' -X POST -H 'Content-Type: application/json' -d '{"name":"markdown","text":"{{ .Bad"}' $API/api/v1/templates/render)
[ "$code" = "422" ] || { echo "FAIL: bad template render returned $code: $(cat /tmp/render-bad.json)"; exit 1; }
echo "templates/render 422 ok"

echo "== phase 3: frigate_meta + templates/defaults + config defaults =="
curl -fsS $API/api/v1/frigate_meta > /tmp/frigate-meta.json
python3 - <<'EOF'
import json
d = json.load(open('/tmp/frigate-meta.json'))
assert d["mqtt"]["enabled"] is True, d
assert d["mqtt"]["host"] == "127.0.0.1", d["mqtt"]
assert d["mqtt"]["port"] == 1883, d["mqtt"]
assert "password" not in d["mqtt"], d["mqtt"]  # Frigate redacts it; never echoed
cams = {c["name"]: c["zones"] for c in d["cameras"]}
assert cams["front_door"] == ["driveway", "yard"], cams
assert cams["back_yard"] == ["patio"], cams
assert d["zones"] == ["driveway", "patio", "yard"], d["zones"]
assert d["labels"] == ["person", "car", "dog"], d["labels"]
assert d["sublabels"] == ["delivery", "mailman"], d["sublabels"]
assert d["version"] == "0.18.2", d
print("frigate_meta ok:", cams)
EOF
curl -fsS $API/api/v1/templates/defaults | python3 -c "import json,sys; d=json.load(sys.stdin); assert {'plaintext','markdown','html'} <= set(d), d; assert 'VERIFY-MARKER' not in d['markdown'], 'defaults must ignore disk edits'; print('templates/defaults ok')"
curl -fsS $API/api/v1/config > /tmp/config-defaults.json
python3 - <<'EOF'
import json
d = json.load(open('/tmp/config-defaults.json'))
assert "defaults" in d, d.keys()
assert d["defaults"]["alerts"]["general"]["title"] == "Frigate Alert", d["defaults"]["alerts"]["general"]
assert d["defaults"]["frigate"]["mqtt"]["port"] == 1883, d["defaults"]["frigate"]["mqtt"]
print("config defaults ok")
EOF

echo "== phase 5 (1): Bark channel =="
# Add a Bark profile pointed at the catcher (/bark path) and test it.
curl -fsS $API/api/v1/config > /tmp/config-got.json
python3 - <<'EOF'
import json
c = json.load(open('/tmp/config-got.json'))["config"]
c["alerts"].setdefault("bark", []).append({
    "enabled": True, "server": "http://127.0.0.1:18081/bark",
    "device_key": "verify-bark-key",
})
json.dump({"config": c}, open('/tmp/config-put.json', 'w'))
EOF
curl -fsS -X PUT -H 'Content-Type: application/json' --data @/tmp/config-put.json $API/api/v1/config | grep -q '"status":"ok"'
sleep 3
curl -fsS -X POST "$API/api/v1/notif_test?provider=bark&index=0" | grep -q '"message":"ok"'
for _ in $(seq 1 30); do
  n=$(wc -l < /tmp/bark-posts.jsonl 2>/dev/null || echo 0)
  [ "$n" -ge 1 ] && break
  sleep 2
done
python3 - <<'EOF'
import json
posts = [json.loads(l) for l in open('/tmp/bark-posts.jsonl') if l.strip()]
assert len(posts) >= 1, posts
body = json.dumps(posts[0], ensure_ascii=False)
assert "title" in posts[0] and posts[0]["title"], posts[0]
assert "body" in posts[0] and posts[0]["body"], posts[0]
assert "verify-bark-key" not in body, "device key must never appear in the Bark JSON body (it travels in the URL path only)"
print("bark ok:", body[:300])
EOF
# Bark profile count must not disturb the webhook counts below.
[ "$(wc -l < /tmp/webhook-posts.jsonl)" -eq 5 ] || { echo "FAIL: webhook count moved during bark test"; exit 1; }

echo "== phase 5 (2): GenAI escalation routing =="
# Narrow normal alerts to webhook#0, add webhook#1 as the escalation target.
curl -fsS $API/api/v1/config > /tmp/config-got.json
python3 - <<'EOF'
import json
c = json.load(open('/tmp/config-got.json'))["config"]
c["alerts"].setdefault("webhook", []).append({
    "enabled": True, "server": "http://127.0.0.1:18081/hook2", "method": "POST",
})
c["alerts"].setdefault("routing", {})["rules"] = [
    {"name": "verify-route", "cameras": ["front_door"],
     "targets": [{"provider": "webhook", "index": 0}]}
]
c["alerts"]["routing"]["escalation"] = [
    {"min_threat": 2, "targets": [{"provider": "webhook", "index": 1}]}
]
json.dump({"config": c}, open('/tmp/config-put.json', 'w'))
EOF
curl -fsS -X PUT -H 'Content-Type: application/json' --data @/tmp/config-put.json $API/api/v1/config | grep -q '"status":"ok"'
sleep 3
wait_hook() {  # wait_hook <file> <want> <timeout_s>
  local file=$1 want=$2 i n
  for i in $(seq 1 "$3"); do
    n=$(wc -l < "$file" 2>/dev/null || echo 0)
    [ "$n" -ge "$want" ] && return 0
    sleep 1
  done
  return 1
}
# Threat 1 update: caption edit on webhook#0 only, no escalation.
python3 test/verify/publish_mqtt.py genai
wait_hook /tmp/webhook-posts.jsonl 6 30 || { echo "FAIL: no 6th webhook post for threat-1 update"; exit 1; }
sleep 2
[ "$(wc -l < /tmp/webhook2-posts.jsonl 2>/dev/null || echo 0)" -eq 0 ] || { echo "FAIL: threat-1 update escalated"; exit 1; }
echo "escalation threat-1 ok: no webhook2 post"
# Threat 2 update: caption edit on webhook#0 AND a fresh alert on webhook#1.
python3 test/verify/publish_mqtt.py genai2
wait_hook /tmp/webhook-posts.jsonl 7 30 || { echo "FAIL: no 7th webhook post for threat-2 update"; exit 1; }
wait_hook /tmp/webhook2-posts.jsonl 1 30 || { echo "FAIL: no escalation post on webhook#1"; exit 1; }
python3 - <<'EOF'
import json
posts = [json.loads(l) for l in open('/tmp/webhook2-posts.jsonl') if l.strip()]
assert len(posts) == 1, posts
assert posts[0].get("genai", {}).get("title") == "有人在撬门", posts[0]
assert posts[0].get("genai", {}).get("threat_level") == "Critical", posts[0]
print("escalation fresh alert ok:", json.dumps(posts[0]["genai"], ensure_ascii=False))
EOF
# Same threat again: caption edit repeats, escalation does NOT (dedup).
python3 test/verify/publish_mqtt.py genai2
wait_hook /tmp/webhook-posts.jsonl 8 30 || { echo "FAIL: no 8th webhook post for repeat threat-2 update"; exit 1; }
sleep 2
[ "$(wc -l < /tmp/webhook2-posts.jsonl)" -eq 1 ] || { echo "FAIL: repeat threat-2 update escalated twice"; exit 1; }
curl -fsS $API/api/v1/history > /tmp/history.json
python3 - <<'EOF'
import json
h = json.load(open('/tmp/history.json'))["history"]
assert any("GenAI 升级提醒" in e.get("detail", "") for e in h), h[:3]
print("escalation history ok")
EOF

echo "== phase 5 (3): scheduled GenAI summary =="
curl -fsS $API/api/v1/config > /tmp/config-got.json
python3 - <<'EOF'
import json
c = json.load(open('/tmp/config-got.json'))["config"]
c["alerts"]["general"].setdefault("genai", {})["schedules"] = [
    {"enabled": True, "time": "20:00", "window": "today",
     "targets": [{"provider": "webhook", "index": 0}]}
]
json.dump({"config": c}, open('/tmp/config-put.json', 'w'))
EOF
curl -fsS -X PUT -H 'Content-Type: application/json' --data @/tmp/config-put.json $API/api/v1/config | grep -q '"status":"ok"'
sleep 3
curl -fsS -X POST -H 'Content-Type: application/json' -d '{"index":0}' $API/api/v1/genai_summarize/run | grep -q '"status":"ok"'
wait_hook /tmp/webhook-posts.jsonl 9 30 || { echo "FAIL: no 9th webhook post for scheduled summary"; exit 1; }
python3 - <<'EOF'
import json
posts = [json.loads(l) for l in open('/tmp/webhook-posts.jsonl') if l.strip()]
ninth = json.dumps(posts[8], ensure_ascii=False)
assert "定时总结测试" in ninth, f"scheduled summary text missing in 9th post: {ninth[:500]}"
print("scheduled summary post ok")
EOF
code=$(curl -s -o /tmp/sched-bad.json -w '%{http_code}' -X POST -H 'Content-Type: application/json' -d '{"index":99}' $API/api/v1/genai_summarize/run)
[ "$code" = "422" ] || { echo "FAIL: bad schedule index returned $code: $(cat /tmp/sched-bad.json)"; exit 1; }
curl -fsS $API/api/v1/history > /tmp/history.json
python3 - <<'EOF'
import json
h = json.load(open('/tmp/history.json'))["history"]
assert any("定时总结" in e.get("detail", "") for e in h), h[:3]
print("scheduled summary history ok")
EOF

echo "== data dir: config + logs persisted =="
if [ -n "${DATA_DIR:-}" ]; then
  test -f "$DATA_DIR/config.yml" || { echo "FAIL: no config.yml in data dir"; exit 1; }
  grep -q "Verify Alert 3" "$DATA_DIR/config.yml" || { echo "FAIL: data-dir config.yml is not the saved config (want title 'Verify Alert 3')"; exit 1; }
  test -s "$DATA_DIR/logs/frigate-notify.log" || { echo "FAIL: no log file in data dir"; exit 1; }
  grep -q "Frigate Notify Plus" "$DATA_DIR/logs/frigate-notify.log" || { echo "FAIL: log file has no startup line"; exit 1; }
  test -f "$DATA_DIR/mute.json" || { echo "FAIL: no mute.json in data dir"; exit 1; }
  test -f "$DATA_DIR/history.jsonl" || { echo "FAIL: no history.jsonl in data dir"; exit 1; }
  grep -q "有人在门口" "$DATA_DIR/history.jsonl" || { echo "FAIL: history.jsonl lacks the GenAI entry"; exit 1; }
  echo "data dir ok: config.yml + logs/frigate-notify.log + mute.json + history.jsonl in $DATA_DIR"
else
  echo "DATA_DIR unset, skipping data-dir assertions"
fi

echo "== proxy phase: webhook delivery transits the forward proxy =="
# Route both webhook profiles at the catcher via a name that only
# resolves on this host, and set the global outbound proxy to the tiny
# proxy on :18082. The name is neither "localhost" nor a loopback IP,
# so it is not bypassed; delivery is impossible except via the proxy.
curl -fsS $API/api/v1/config > /tmp/config-proxy-backup.json
python3 - <<'EOF'
import json
c = json.load(open('/tmp/config-proxy-backup.json'))["config"]
c["alerts"]["general"]["proxy"] = {"url": "http://127.0.0.1:18082"}
c["alerts"]["webhook"][0]["server"] = "http://webhook-catcher:18081/hook"
c["alerts"]["webhook"][1]["server"] = "http://webhook-catcher:18081/hook2"
json.dump({"config": c}, open('/tmp/config-put.json', 'w'))
EOF
curl -fsS -X PUT -H 'Content-Type: application/json' --data @/tmp/config-put.json $API/api/v1/config | grep -q '"status":"ok"'
sleep 3
publish_genai_expect 10
grep -q "POST http://webhook-catcher:18081/hook" /tmp/proxy-hits.log || { echo "FAIL: webhook delivery did not transit the proxy"; cat /tmp/proxy-hits.log; exit 1; }
posts_after_transit=$(wc -l < /tmp/webhook-posts.jsonl)
echo "proxy transit ok: webhook post #$posts_after_transit arrived via the forward proxy"

echo "== proxy phase (2): dead proxy fails the delivery, no direct fallback =="
python3 - <<'EOF'
import json
c = json.load(open('/tmp/config-proxy-backup.json'))["config"]
c["alerts"]["general"]["proxy"] = {"url": "http://127.0.0.1:9"}
c["alerts"]["webhook"][0]["server"] = "http://webhook-catcher:18081/hook"
c["alerts"]["webhook"][1]["server"] = "http://webhook-catcher:18081/hook2"
json.dump({"config": c}, open('/tmp/config-put.json', 'w'))
EOF
curl -fsS -X PUT -H 'Content-Type: application/json' --data @/tmp/config-put.json $API/api/v1/config | grep -q '"status":"ok"'
sleep 3
failed_before=$(curl -fsS $API/api/v1/status | python3 -c "import json,sys; print(json.load(sys.stdin)['status']['notifications']['webhook'][0]['failed'])")
curl -fsS -X POST "$API/api/v1/notif_test?provider=webhook&index=0" > /dev/null
deadline=$((SECONDS+120))
while :; do
  failed_now=$(curl -fsS $API/api/v1/status | python3 -c "import json,sys; print(json.load(sys.stdin)['status']['notifications']['webhook'][0]['failed'])")
  [ "$failed_now" -gt "$failed_before" ] && break
  [ $SECONDS -ge $deadline ] && { echo "FAIL: dead-proxy delivery never recorded a failure"; exit 1; }
  sleep 3
done
n=$(wc -l < /tmp/webhook-posts.jsonl)
[ "$n" -eq "$posts_after_transit" ] || { echo "FAIL: dead-proxy delivery fell back to direct (webhook posts $n, want $posts_after_transit)"; exit 1; }
curl -fsS $API/api/v1/status | python3 -c "
import json,sys
w = json.load(sys.stdin)['status']['notifications']['webhook'][0]
assert '127.0.0.1:9' in w['last_error'], w
print('dead-proxy ok: failure recorded, last_error names the proxy:', w['last_error'][:120])
"
# Restore the pre-proxy config for the steps that follow.
python3 - <<'EOF'
import json
c = json.load(open('/tmp/config-proxy-backup.json'))["config"]
json.dump({"config": c}, open('/tmp/config-put.json', 'w'))
EOF
curl -fsS -X PUT -H 'Content-Type: application/json' --data @/tmp/config-put.json $API/api/v1/config | grep -q '"status":"ok"'
sleep 3
echo "proxy config restored"

echo "ALL CHECKS PASSED"
