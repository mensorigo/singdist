#!/usr/bin/env python3
"""Local execution-layer harness for the new sync engine (NOT the
scenario suite).

Covers what the suite cannot (it runs with no core unit): fake
`systemctl` on PATH (restart/is-active driven by a mode file) + a
fake Clash HTTP server (/version -> 200) + a fake sing-box checker,
against a real panel on 127.0.0.1:18098.

Tests:
  T1 debounce batching: 3 rapid panel edits -> ONE auto-sync / ONE restart
  T2 mutex skip: concurrent manual sync during a slow sync -> skipped
  T3 check-fail abort: always-fail checker -> check_failed, 0 restarts,
     target bytes unchanged
  T4 atomic write: no temp files left, target valid JSON after success
  T5 poll health speed: instant-healthy sync completes in <5s
  T6 retry+rollback: fail-first-3 restarts -> 3 attempts + rollback
     restart succeeds -> restart_failed row, target == baseline bytes,
     input blocked
  T7 rollback-failure: always-fail restarts -> 4 calls total, no loop,
     target stays at baseline bytes
  T8 anti-loop block: blocked input auto-skips (no rows/calls); a new
     panel edit unblocks and auto-applies

Usage: python3 tests/local_exec_harness.py [T1 T2 ...]  (no args = all)
Exit code 0 = all checks passed.

Test-only values: the Reality keypair and passwords exist only here.
"""

import http.client
import http.cookiejar
import http.server
import json
import os
import re
import shutil
import socketserver
import ssl
import sqlite3
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = os.path.join(REPO, "bin", "singbox-ui")
PORT = 18098
CLASH_PORT = 19091
BASE = f"https://127.0.0.1:{PORT}"
DATA = "/tmp/exectest"
TARGET = os.path.join(DATA, "target.json")
FAKEBIN_DIR = "/tmp/exectest-bin"
CERT_DIR = "/tmp/exectest-cert"
MODE_FILE = os.path.join(DATA, "systemctl-mode")
CALLS_FILE = os.path.join(DATA, "restart-calls.log")
STATE_FILE = os.path.join(DATA, "systemctl-state")
CHECK_FAIL_FILE = os.path.join(DATA, "check-always-fail")

REAL_PRIV = "1yslk4XzXdcvAy7cnI-0V1upocRCOtiO--clg70Bmzs"
REAL_PUB = "FbhlNk4fGjs4Un_fKM7hOMrsETQ-9QioqNERrBPHTiE"
REAL_SHORT = "abcd1234"

RESULTS = []


def check(scen, name, cond, detail=""):
    RESULTS.append((scen, name, bool(cond), detail))
    mark = "ok  " if cond else "FAIL"
    line = f"  [{mark}] {name}"
    if not cond and detail:
        line += f" -- {detail}"
    print(line)


def ensure_fakebin():
    os.makedirs(FAKEBIN_DIR, exist_ok=True)
    os.makedirs(CERT_DIR, exist_ok=True)
    checker = os.path.join(FAKEBIN_DIR, "sing-box")
    if not os.path.exists(checker):
        with open(checker, "w") as f:
            f.write("""#!/bin/bash
# Fake sing-box checker: `check -c <file>`. Fails on CHECK_FAIL_MARKER
# or when the always-fail marker file exists.
if [ "$1" = "check" ] && [ "$2" = "-c" ]; then
  if [ -f "/tmp/exectest/check-always-fail" ]; then
    echo "fake check: always-fail marker" >&2; exit 1
  fi
  if grep -q "CHECK_FAIL_MARKER" "$3" 2>/dev/null; then
    echo "fake check: marker found" >&2; exit 1
  fi
  python3 -c "import json,sys; json.load(open(sys.argv[1]))" "$3" || exit 1
  exit 0
fi
echo "unsupported fake-sing-box call: $*" >&2; exit 1
""")
        os.chmod(checker, 0o755)
    ctl = os.path.join(FAKEBIN_DIR, "systemctl")
    with open(ctl, "w") as f:
        f.write("""#!/bin/bash
# Fake systemctl: restart/is-active/show driven by /tmp/exectest files.
# Mode file values: ok | slow:N | fail-first-N | always-fail
# show -p MainPID: while the target contains CONFLICT, each call
# returns a NEW pid (simulating systemd's ~100ms respawn flap on a
# bind-conflict config); otherwise a stable pid. This exercises the
# panel's PID-stability fail-fast: without it, the 500ms health poll
# can miss every brief "failed" window and burn 30s per attempt.
DATA=/tmp/exectest
if [ "$1" = "show" ]; then
  if grep -q "CONFLICT" "$DATA/target.json" 2>/dev/null; then
    c=$(cat "$DATA/pid-counter" 2>/dev/null || echo 10000)
    c=$((c+1)); echo "$c" > "$DATA/pid-counter"
    echo "MainPID=$c"
  else
    echo "MainPID=5555"
  fi
  exit 0
fi
if [ "$1" = "restart" ]; then
  echo "$(date +%s.%N) restart $2" >> "$DATA/restart-calls.log"
  mode=$(cat "$DATA/systemctl-mode" 2>/dev/null || echo ok)
  case "$mode" in
    slow:*)
      sleep "${mode#slow:}"; echo active > "$DATA/systemctl-state"; exit 0;;
    fail-first-*)
      n="${mode#fail-first-}"
      count=$(wc -l < "$DATA/restart-calls.log")
      if [ "$count" -le "$n" ]; then echo failed > "$DATA/systemctl-state"; exit 1; fi
      echo active > "$DATA/systemctl-state"; exit 0;;
    always-fail)
      echo failed > "$DATA/systemctl-state"; exit 1;;
    *)
      echo active > "$DATA/systemctl-state"; exit 0;;
  esac
fi
if [ "$1" = "is-active" ]; then
  cat "$DATA/systemctl-state" 2>/dev/null || echo active
  exit 0
fi
echo "unsupported fake-systemctl call: $*" >&2; exit 1
""")
    os.chmod(ctl, 0o755)
    cert = os.path.join(CERT_DIR, "cert.pem")
    key = os.path.join(CERT_DIR, "key.pem")
    if not (os.path.exists(cert) and os.path.exists(key)):
        subprocess.run(
            ["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
             "-keyout", key, "-out", cert, "-days", "365",
             "-subj", "/CN=localhost"],
            check=True, capture_output=True)


class ClashHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        # A flapping core never serves /version (it dies at inbound
        # start): while the target holds CONFLICT, answer 503.
        broken = False
        try:
            with open(TARGET) as f:
                broken = "CONFLICT" in f.read()
        except Exception:
            pass
        if self.path == "/version" and not broken:
            body = json.dumps({"meta": True, "premium": True,
                               "version": "sing-box 1.14.3"}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        else:
            self.send_response(404)
            self.end_headers()

    def log_message(self, *a):
        pass


def start_clash():
    socketserver.TCPServer.allow_reuse_address = True
    srv = socketserver.TCPServer(("127.0.0.1", CLASH_PORT), ClashHandler)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


class Panel:
    def __init__(self, auto=True):
        shutil.rmtree(DATA, ignore_errors=True)
        os.makedirs(DATA, exist_ok=True)
        with open(MODE_FILE, "w") as f:
            f.write("ok")
        with open(STATE_FILE, "w") as f:
            f.write("active")
        self.bootlog = os.path.join(DATA, "boot.log")
        self.logf = open(self.bootlog, "w")
        env = dict(os.environ)
        env["PATH"] = FAKEBIN_DIR + ":" + env.get("PATH", "")
        env.update({
            "SINGBOX_UI_ADDR": f"127.0.0.1:{PORT}",
            "SINGBOX_UI_DATA_DIR": DATA,
            "SINGBOX_UI_SINGBOX_CONFIG": TARGET,
            "SINGBOX_UI_SYNC_CONFIG": TARGET,
            "SINGBOX_UI_SINGBOX_BIN": os.path.join(FAKEBIN_DIR, "sing-box"),
            "SINGBOX_UI_CORE_UNIT": "sing-box",
            "SINGBOX_UI_CLASH_API": f"http://127.0.0.1:{CLASH_PORT}",
            "SINGBOX_UI_TLS_CERT": os.path.join(CERT_DIR, "cert.pem"),
            "SINGBOX_UI_TLS_KEY": os.path.join(CERT_DIR, "key.pem"),
            "SINGBOX_UI_AUTO_SYNC_MINUTES": "0",
        })
        self.proc = subprocess.Popen(
            [BIN], env=env, stdout=self.logf, stderr=subprocess.STDOUT)
        try:
            self.jar = http.cookiejar.CookieJar()
            self.opener = urllib.request.build_opener(
                urllib.request.HTTPCookieProcessor(self.jar),
                urllib.request.HTTPSHandler(
                    context=ssl._create_unverified_context()))
            self._wait_ready()
            self._login()
            if not auto:
                self.api("PATCH", "/api/settings", {"autoSyncEnabled": False})
        except Exception:
            self.stop()
            raise

    def _req(self, method, path, body=None):
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(BASE + path, data=data, method=method)
        if data is not None:
            req.add_header("Content-Type", "application/json")
        try:
            with self.opener.open(req, timeout=30) as resp:
                raw = resp.read().decode()
                return resp.status, json.loads(raw) if raw else None
        except urllib.error.HTTPError as e:
            raw = e.read().decode()
            try:
                return e.code, json.loads(raw)
            except ValueError:
                return e.code, raw

    def api(self, method, path, body=None, expect=200):
        st, v = self._req(method, path, body)
        assert st == expect, f"{method} {path} -> {st} (want {expect}): {v}"
        return v

    def _wait_ready(self):
        for _ in range(60):
            try:
                req = urllib.request.Request(BASE + "/")
                with self.opener.open(req, timeout=5) as resp:
                    if resp.status == 200:
                        return
            except Exception:
                pass
            time.sleep(0.5)
        raise RuntimeError("panel did not become ready")

    def _login(self):
        self.logf.flush()
        with open(self.bootlog) as f:
            log = f.read()
        m = re.search(r"password:\s*(\S+)", log)
        assert m, "first-run password not found in boot log"
        self.api("POST", "/api/auth/login",
                 {"username": "singbox-ui", "password": m.group(1)})

    def stop(self):
        self.proc.terminate()
        try:
            self.proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.proc.kill()
        self.logf.close()

    def mk_inbound(self, name, protocol, port, params):
        return self.api("POST", "/api/inbounds",
                        {"name": name, "protocol": protocol, "port": port,
                         "params": params}, expect=201)

    def mk_template(self, name, inbound_ids):
        return self.api("POST", "/api/templates",
                        {"name": name, "inboundIds": inbound_ids}, expect=201)

    def mk_sub(self, name, template_id):
        return self.api("POST", "/api/subscriptions",
                        {"name": name, "templateId": template_id,
                         "limitBytes": 0, "expireAt": 0, "maxIps": 0},
                        expect=201)

    def sync(self):
        return self.api("POST", "/api/sync", {})

    def sync_log(self, limit=50):
        return self.api("GET", f"/api/sync/log?limit={limit}")

    def inbounds(self):
        return self.api("GET", "/api/inbounds")["items"]

    def inbound_named(self, name):
        for ib in self.inbounds():
            if ib["name"] == name:
                return ib
        return None


def read_target():
    with open(TARGET) as f:
        return json.load(f)


def read_target_bytes():
    with open(TARGET, "rb") as f:
        return f.read()


def set_mode(mode):
    with open(MODE_FILE, "w") as f:
        f.write(mode)
    # Reset the call counter so fail-first-N counts from this mode,
    # not from earlier phases (e.g. the converge restart).
    with open(CALLS_FILE, "w") as f:
        pass


def restart_calls():
    if not os.path.exists(CALLS_FILE):
        return 0
    with open(CALLS_FILE) as f:
        return len([l for l in f if l.strip()])


def wait_for(pred, timeout=60, interval=1):
    deadline = time.time() + timeout
    while time.time() < deadline:
        v = pred()
        if v:
            return v
        time.sleep(interval)
    return None


def vless_params(sni):
    return {"sni": sni, "privateKey": REAL_PRIV, "publicKey": REAL_PUB,
            "shortId": REAL_SHORT, "flow": "xtls-rprx-vision"}


def setup_basic(p):
    v = p.mk_inbound("V", "vless-reality", 26000, vless_params("h.example.com"))
    t = p.mk_template("T", [v["id"]])
    p.mk_sub("SUB", t["id"])
    return v


def t1_debounce(p):
    setup_basic(p)
    auto = wait_for(lambda: next(
        (r for r in p.sync_log(20)
         if r["action"] == "auto-sync" and r["result"] == "applied"), None),
        timeout=40)
    check("T1", "auto converge applied", auto is not None, str(p.sync_log(10)))
    base_calls = restart_calls()
    v = p.inbound_named("V")
    params = v["params"]
    for port in (26001, 26002, 26003):
        p.api("PATCH", f"/api/inbounds/{v['id']}",
              {"name": "V", "port": port, "params": params})
        time.sleep(0.3)
    pushed = wait_for(lambda: len([r for r in p.sync_log(20)
         if r["action"] == "auto-sync" and r["result"] == "applied"]) >= 2,
        timeout=40)
    check("T1", "one batched auto push after rapid edits", pushed is not None)
    time.sleep(7)  # let any extra push land if batching failed
    autos = [r for r in p.sync_log(20)
             if r["action"] == "auto-sync" and r["result"] == "applied"]
    check("T1", "exactly 2 auto applied rows (converge + batch)",
          len(autos) == 2, str(autos))
    check("T1", "exactly 1 restart for the batch",
          restart_calls() - base_calls == 1,
          f"base={base_calls} now={restart_calls()}")
    check("T1", "target port is last edit",
          read_target()["inbounds"][0]["listen_port"] == 26003)


def t2_mutex(p):
    setup_basic(p)
    row = p.sync()
    check("T2", "converge applied", row["result"] == "applied", str(row))
    set_mode("slow:5")
    v = p.inbound_named("V")
    p.api("PATCH", f"/api/inbounds/{v['id']}",
          {"name": "V", "port": 26010, "params": v["params"]})
    results = {}

    def run_sync():
        results["first"] = p.sync()

    th = threading.Thread(target=run_sync)
    th.start()
    time.sleep(1)  # first sync inside its slow restart
    second = p.sync()
    th.join(timeout=30)
    check("T2", "first sync applied", results.get("first", {}).get("result") == "applied",
          str(results))
    check("T2", "concurrent sync skipped (not queued)",
          second["result"] == "skipped", str(second))
    set_mode("ok")


def t3_check_fail(p):
    setup_basic(p)
    row = p.sync()
    check("T3", "converge applied", row["result"] == "applied", str(row))
    before = read_target_bytes()
    calls_before = restart_calls()
    with open(CHECK_FAIL_FILE, "w") as f:
        f.write("1")
    try:
        v = p.inbound_named("V")
        p.api("PATCH", f"/api/inbounds/{v['id']}",
              {"name": "V", "port": 26020, "params": v["params"]})
        row = p.sync()
    finally:
        os.remove(CHECK_FAIL_FILE)
    check("T3", "check-fail aborts with check_failed",
          row["result"] == "check_failed", str(row))
    check("T3", "no restart on check failure",
          restart_calls() == calls_before,
          f"before={calls_before} now={restart_calls()}")
    check("T3", "target bytes unchanged", read_target_bytes() == before)


def t4_atomic_write(p):
    setup_basic(p)
    row = p.sync()
    check("T4", "converge applied", row["result"] == "applied", str(row))
    leftovers = [f for f in os.listdir(DATA)
                 if "tmp" in f.lower() or f.endswith(".tmp")]
    check("T4", "no temp files left in data dir", leftovers == [], str(leftovers))
    try:
        doc = read_target()
        ok = doc["inbounds"][0]["tag"] == "V"
    except Exception as e:
        ok = False
        doc = str(e)
    check("T4", "target valid JSON with V", ok, str(doc))


def t5_poll_speed(p):
    setup_basic(p)
    row = p.sync()
    check("T5", "converge applied", row["result"] == "applied", str(row))
    v = p.inbound_named("V")
    p.api("PATCH", f"/api/inbounds/{v['id']}",
          {"name": "V", "port": 26030, "params": v["params"]})
    t0 = time.time()
    row = p.sync()
    dt = time.time() - t0
    check("T5", "sync applied", row["result"] == "applied", str(row))
    check("T5", "poll health completes fast (<5s, not fixed 10s waits)",
          dt < 5, f"{dt:.2f}s")


def t6_retry_rollback(p):
    setup_basic(p)
    row = p.sync()
    check("T6", "converge applied", row["result"] == "applied", str(row))
    baseline = read_target()
    baseline_bytes = read_target_bytes()
    set_mode("fail-first-3")
    v = p.inbound_named("V")
    p.api("PATCH", f"/api/inbounds/{v['id']}",
          {"name": "V", "port": 26040, "params": v["params"]})
    calls_before = restart_calls()
    row = p.sync()
    check("T6", "rollback sync result restart_failed",
          row["result"] == "restart_failed", str(row))
    check("T6", "3 attempts + 1 rollback restart = 4 calls",
          restart_calls() - calls_before == 4,
          f"before={calls_before} now={restart_calls()}")
    check("T6", "target rolled back to baseline bytes",
          read_target_bytes() == baseline_bytes)
    check("T6", "target port is baseline port",
          read_target()["inbounds"][0]["listen_port"] == 26000)
    rows = p.api("GET", "/api/sync/changes?limit=200")
    notes = [r for r in rows if r["syncLogId"] == row["id"]
             and "已回退到本轮之前的版本" in r.get("detail", "")]
    check("T6", "rollback note row present", len(notes) == 1, str(notes))
    db = os.path.join(DATA, "singbox-ui.db")
    con = sqlite3.connect(db)
    blocked = con.execute(
        "SELECT blocked_hash FROM sync_state LIMIT 1").fetchone()[0]
    con.close()
    check("T6", "input blocked after rollback",
          blocked != "" and blocked == row["hash"], f"blocked={blocked!r}")
    set_mode("ok")


def t7_rollback_failure(p):
    setup_basic(p)
    row = p.sync()
    check("T7", "converge applied", row["result"] == "applied", str(row))
    baseline_bytes = read_target_bytes()
    set_mode("always-fail")
    v = p.inbound_named("V")
    p.api("PATCH", f"/api/inbounds/{v['id']}",
          {"name": "V", "port": 26050, "params": v["params"]})
    calls_before = restart_calls()
    row = p.sync()
    check("T7", "rollback-failure result restart_failed",
          row["result"] == "restart_failed", str(row))
    check("T7", "4 calls total, no infinite loop",
          restart_calls() - calls_before == 4,
          f"before={calls_before} now={restart_calls()}")
    check("T7", "file stays at baseline version",
          read_target_bytes() == baseline_bytes)
    rows = p.api("GET", "/api/sync/changes?limit=200")
    notes = [r for r in rows if r["syncLogId"] == row["id"]
             and "回退也失败" in r.get("detail", "")]
    check("T7", "rollback-failure note row present", len(notes) == 1, str(notes))
    set_mode("ok")


def t8_anti_loop(p):
    # Same panel as T6 would be ideal; this harness uses a fresh panel
    # per test, so rebuild the blocked state here first.
    setup_basic(p)
    row = p.sync()
    check("T8", "converge applied", row["result"] == "applied", str(row))
    set_mode("fail-first-3")
    v = p.inbound_named("V")
    p.api("PATCH", f"/api/inbounds/{v['id']}",
          {"name": "V", "port": 26060, "params": v["params"]})
    row = p.sync()
    check("T8", "blocked state established", row["result"] == "restart_failed",
          str(row))
    set_mode("ok")
    p.api("PATCH", "/api/settings", {"autoSyncEnabled": True})
    calls_before = restart_calls()
    rows_before = len([r for r in p.sync_log(20) if r["action"] == "auto-sync"])
    time.sleep(16)  # drift poll 5s + debounce 5s + margin
    rows_after = len([r for r in p.sync_log(20) if r["action"] == "auto-sync"])
    check("T8", "blocked input auto-skips (no new auto rows)",
          rows_after == rows_before, f"before={rows_before} after={rows_after}")
    check("T8", "blocked input auto-skips (no new restarts)",
          restart_calls() == calls_before,
          f"before={calls_before} now={restart_calls()}")
    v = p.inbound_named("V")
    p.api("PATCH", f"/api/inbounds/{v['id']}",
          {"name": "V", "port": 26061, "params": v["params"]})
    pushed = wait_for(lambda: next(
        (r for r in p.sync_log(20)
         if r["action"] == "auto-sync" and r["result"] == "applied"), None),
        timeout=40)
    check("T8", "new input unblocks and auto-applies", pushed is not None)
    check("T8", "restart happened for new input",
          restart_calls() > calls_before,
          f"before={calls_before} now={restart_calls()}")


def t9_flap(p):
    # Bind-conflict flap: the core respawns rapidly (pid changes every
    # sample) instead of settling to "failed". The panel must fail
    # fast on PID change, retry, roll back — in seconds, not 3x30s.
    setup_basic(p)
    row = p.sync()
    check("T9", "converge applied", row["result"] == "applied", str(row))
    baseline_bytes = read_target_bytes()
    doc = read_target()
    doc["inbounds"].append({"tag": "CONFLICT", "listen": "::",
                            "listen_port": 26000, "type": "socks",
                            "users": []})
    with open(TARGET, "w") as f:
        json.dump(doc, f, indent=2)
    t0 = time.time()
    row = p.sync()
    dt = time.time() - t0
    check("T9", "flap sync result restart_failed",
          row["result"] == "restart_failed", str(row))
    check("T9", "flap fail-fast (seconds, not 3x30s health deadlines)",
          dt < 30, f"{dt:.2f}s")
    check("T9", "target rolled back to baseline bytes",
          read_target_bytes() == baseline_bytes)
    rows = p.api("GET", "/api/sync/changes?limit=200")
    notes = [r for r in rows if r["syncLogId"] == row["id"]
             and "已回退到本轮之前的版本" in r.get("detail", "")]
    check("T9", "rollback note row present", len(notes) == 1, str(notes))
    db = os.path.join(DATA, "singbox-ui.db")
    con = sqlite3.connect(db)
    blocked = con.execute(
        "SELECT blocked_hash FROM sync_state LIMIT 1").fetchone()[0]
    con.close()
    check("T9", "input blocked after rollback",
          blocked != "" and blocked == row["hash"], f"blocked={blocked!r}")


def t10_stale_block_cleared(p):
    # Block established manually, then the operator fixes the FILE by
    # hand (conflict removed): the system converges, the drift poll
    # triggers a run, AutoSync clears the stale block — with NO new
    # auto row (InSync produces no rows by design).
    setup_basic(p)
    row = p.sync()
    check("T10", "converge applied", row["result"] == "applied", str(row))
    doc = read_target()
    doc["inbounds"].append({"tag": "CONFLICT", "listen": "::",
                            "listen_port": 26000, "type": "socks",
                            "users": []})
    with open(TARGET, "w") as f:
        json.dump(doc, f, indent=2)
    row = p.sync()
    check("T10", "blocked state established", row["result"] == "restart_failed",
          str(row))
    db = os.path.join(DATA, "singbox-ui.db")

    def blocked():
        con = sqlite3.connect(db)
        v = con.execute(
            "SELECT blocked_hash FROM sync_state LIMIT 1").fetchone()[0]
        con.close()
        return v

    check("T10", "block set", blocked() != "")
    # Operator fixes the file by hand (no panel edit, no new input).
    doc = read_target()
    doc["inbounds"] = [ib for ib in doc["inbounds"]
                       if ib["tag"] != "CONFLICT"]
    with open(TARGET, "w") as f:
        json.dump(doc, f, indent=2)
    p.api("PATCH", "/api/settings", {"autoSyncEnabled": True})
    cleared = wait_for(lambda: blocked() == "", timeout=40, interval=2)
    check("T10", "stale block cleared on convergence", cleared is not None,
          f"blocked={blocked()!r}")
    autos = [r for r in p.sync_log(20) if r["action"] == "auto-sync"]
    check("T10", "no new auto rows for the convergence",
          autos == [], str(autos))


TESTS = [
    ("T1", t1_debounce, True), ("T2", t2_mutex, False),
    ("T3", t3_check_fail, False), ("T4", t4_atomic_write, False),
    ("T5", t5_poll_speed, False), ("T6", t6_retry_rollback, False),
    ("T7", t7_rollback_failure, False), ("T8", t8_anti_loop, False),
    ("T9", t9_flap, False), ("T10", t10_stale_block_cleared, False),
]


def main():
    ensure_fakebin()
    assert os.path.exists(BIN), f"binary missing: {BIN} (run ./build.sh first)"
    start_clash()
    wanted = sys.argv[1:]
    todo = [t for t in TESTS if not wanted or t[0] in wanted]
    if not todo:
        print("no matching tests")
        return 2
    for name, fn, auto in todo:
        print(f"== {name} ==")
        panel = None
        try:
            panel = Panel(auto=auto)
            fn(panel)
        except Exception as e:
            import traceback
            check(name, "test ran without harness error", False,
                  f"{e}\n{traceback.format_exc(limit=3)}")
        finally:
            if panel:
                panel.stop()
    failed = [r for r in RESULTS if not r[2]]
    print(f"\n{len(RESULTS) - len(failed)}/{len(RESULTS)} checks passed, "
          f"{len(failed)} failed")
    for scen, name, _, detail in failed:
        print(f"  FAILED {scen}: {name} -- {detail}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
