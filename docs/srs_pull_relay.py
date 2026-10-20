#!/usr/bin/env python3
"""SRS -> Node-RED relay (for Node-RED installs that only have `tcp in`).

Runs on the on-prem mini PC. Every POLL_SECONDS it pulls ON/OFF commands from
SRS over HTTPS, sends each valid command to Node-RED over local TCP as ONE
JSON line, waits for Node-RED's one-line JSON reply and acknowledges it to SRS.
Python 3.8+, standard library only.

Environment:
  SRS_PULL_KEY      (required) API key for the pull endpoints
  SRS_BASE          default https://unesaroombooking.mekansm.co.id/api/integration/v1/automation
  NODERED_HOST      default 127.0.0.1
  NODERED_PORT      default 5051        (the `tcp in` node, server mode)
  POLL_SECONDS      default 10
  REPLY_TIMEOUT     default 25          (seconds to wait for Node-RED's reply)
  DRY_RUN           1 = never send to Node-RED, only print the commands (default 0)

Line sent to Node-RED:  {"commandId":..,"roomCode":..,"classKey":..,"action":"on|off","eventId":..,"bookingId":..,"source":"room_booking"}
Reply expected (one line): {"success":true,"devices":{"ac":"command_sent","lamp":"command_sent","door":"command_sent"},"message":""}
"""
import json
import os
import socket
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone

KEY = os.environ.get("SRS_PULL_KEY", "")
BASE = os.environ.get("SRS_BASE", "https://unesaroombooking.mekansm.co.id/api/integration/v1/automation").rstrip("/")
NR_HOST = os.environ.get("NODERED_HOST", "127.0.0.1")
NR_PORT = int(os.environ.get("NODERED_PORT", "5051"))
POLL = float(os.environ.get("POLL_SECONDS", "10"))
REPLY_TIMEOUT = float(os.environ.get("REPLY_TIMEOUT", "25"))
DRY_RUN = os.environ.get("DRY_RUN", "0") == "1"
INFLIGHT_SECONDS = 60

inflight = {}  # commandId -> epoch when it may be sent again


def log(msg):
    print(datetime.now().strftime("%Y-%m-%d %H:%M:%S"), msg, flush=True)


def http(method, path, body=None):
    req = urllib.request.Request(BASE + path, method=method)
    req.add_header("X-API-Key", KEY)
    data = None
    if body is not None:
        req.add_header("Content-Type", "application/json")
        data = json.dumps(body).encode()
    try:
        with urllib.request.urlopen(req, data=data, timeout=15) as r:
            return r.status, json.loads(r.read() or b"{}")
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read() or b"{}")
        except Exception:
            return e.code, {}


def valid_now(cmd):
    now = datetime.now(timezone.utc)
    try:
        start = datetime.fromisoformat(cmd["validFrom"])
        end = datetime.fromisoformat(cmd["expiresAt"])
    except Exception:
        return False
    return start <= now < end


def ask_nodered(cmd):
    """Send one JSON line, return the parsed one-line reply (or None)."""
    line = json.dumps({
        "commandId": cmd["commandId"], "roomCode": cmd["roomCode"], "classKey": cmd["classKey"],
        "action": cmd["action"], "eventId": cmd["eventId"], "bookingId": cmd["bookingId"],
        "source": cmd.get("source", "room_booking"),
    }) + "\n"
    with socket.create_connection((NR_HOST, NR_PORT), timeout=5) as s:
        s.settimeout(REPLY_TIMEOUT)
        s.sendall(line.encode())
        buf = b""
        while b"\n" not in buf:
            chunk = s.recv(4096)
            if not chunk:
                break
            buf += chunk
    text = buf.split(b"\n")[0].decode(errors="replace").strip()
    return json.loads(text) if text else None


def handle(cmd):
    cid = cmd["commandId"]
    if not valid_now(cmd):
        log(f"skip {cid}: outside validFrom/expiresAt")
        return
    if inflight.get(cid, 0) > time.time():
        return
    inflight[cid] = time.time() + INFLIGHT_SECONDS
    if DRY_RUN:
        log(f"[dry-run] {cmd['action'].upper()} {cmd['roomCode']} ({cid}) - not sent to Node-RED, no ack")
        return
    log(f"{cmd['action'].upper()} {cmd['roomCode']} ({cid}) -> Node-RED")
    try:
        reply = ask_nodered(cmd)
        if reply is None:
            raise RuntimeError("empty reply")
        ack = {"success": bool(reply.get("success")), "devices": reply.get("devices", {}), "message": reply.get("message", "")}
    except Exception as e:  # unreachable Node-RED, timeout, bad JSON ...
        log(f"  Node-RED failed: {e}")
        ack = {"success": False, "devices": {}, "message": f"relay: {e}"}
    status, body = http("POST", f"/commands/{cid}/ack", ack)
    log(f"  ack -> HTTP {status} {body.get('data', body.get('error', ''))}")


def main():
    if not KEY:
        sys.exit("SRS_PULL_KEY is not set")
    log(f"relay started: {BASE} -> {NR_HOST}:{NR_PORT} every {POLL:.0f}s (dry_run={DRY_RUN})")
    while True:
        try:
            status, body = http("GET", "/commands")
            if status == 200:
                for cmd in body.get("data", {}).get("commands", []):
                    handle(cmd)
            else:
                log(f"GET /commands -> HTTP {status}")
        except Exception as e:
            log(f"poll error: {e}")
        time.sleep(POLL)


if __name__ == "__main__":
    main()
