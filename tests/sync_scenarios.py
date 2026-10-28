#!/usr/bin/env python3
"""Local sync scenario suite for singbox-ui.

Runs a fresh panel instance per scenario on 127.0.0.1:18099 (fake
sing-box checker, self-signed cert, no core restart) and drives it
through the sync rule: strict classification, passthrough, shared
editable imports with a change log, illegal auto-overwrite.

Usage: python3 tests/sync_scenarios.py [S0 S1 ...]  (no args = all)
Exit code 0 = all checks passed.

Test-only values (never production secrets): the Reality keypair and
all passwords below exist only in these scenarios.
"""

import base64
import http.client
import http.cookiejar
import json
import os
import re
import shutil
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = os.path.join(REPO, "bin", "singbox-ui")
PORT = 18099
BASE = f"https://127.0.0.1:{PORT}"
DATA = "/tmp/synctest"
TARGET = os.path.join(DATA, "target.json")
CERT_DIR = "/tmp/synctest-cert"
FAKEBIN_DIR = "/tmp/synctest-bin"
FAKEBIN = os.path.join(FAKEBIN_DIR, "fake-sing-box")

# Test-only Reality keypair (panel validation requires a matching pair).
REAL_PRIV = "1yslk4XzXdcvAy7cnI-0V1upocRCOtiO--clg70Bmzs"
REAL_PUB = "FbhlNk4fGjs4Un_fKM7hOMrsETQ-9QioqNERrBPHTiE"
REAL_SHORT = "abcd1234"

RESULTS = []  # (scenario, check, ok, detail)


def check(scen, name, cond, detail=""):
    RESULTS.append((scen, name, bool(cond), detail))
    mark = "ok  " if cond else "FAIL"
    line = f"  [{mark}] {name}"
    if not cond and detail:
        line += f" -- {detail}"
    print(line)


def ensure_fakebin():
    os.makedirs(FAKEBIN_DIR, exist_ok=True)
    if not os.path.exists(FAKEBIN):
        with open(FAKEBIN, "w") as f:
            f.write("""#!/bin/bash
# Fake sing-box: only `check -c <file>`. Parses JSON; fails when the
# file contains CHECK_FAIL_MARKER (simulates a semantic check failure).
if [ "$1" = "check" ] && [ "$2" = "-c" ]; then
  if grep -q "CHECK_FAIL_MARKER" "$3" 2>/dev/null; then
    echo "fake check: marker found" >&2; exit 1
  fi
  python3 -c "import json,sys; json.load(open(sys.argv[1]))" "$3" || exit 1
  exit 0
fi
echo "unsupported fake-sing-box call: $*" >&2; exit 1
""")
        os.chmod(FAKEBIN, 0o755)
    os.makedirs(CERT_DIR, exist_ok=True)
    cert = os.path.join(CERT_DIR, "cert.pem")
    key = os.path.join(CERT_DIR, "key.pem")
    if not (os.path.exists(cert) and os.path.exists(key)):
        subprocess.run(
            ["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
             "-keyout", key, "-out", cert, "-days", "365",
             "-subj", "/CN=localhost"],
            check=True, capture_output=True)


class Panel:
    """One fresh panel instance."""

    def __init__(self, auto_sync_min=0):
        shutil.rmtree(DATA, ignore_errors=True)
        os.makedirs(DATA, exist_ok=True)
        self.bootlog = os.path.join(DATA, "boot.log")
        self.logf = open(self.bootlog, "w")
        env = dict(os.environ)
        env.update({
            "SINGBOX_UI_ADDR": f"127.0.0.1:{PORT}",
            "SINGBOX_UI_DATA_DIR": DATA,
            "SINGBOX_UI_SINGBOX_CONFIG": TARGET,
            "SINGBOX_UI_SYNC_CONFIG": TARGET,
            "SINGBOX_UI_SINGBOX_BIN": FAKEBIN,
            "SINGBOX_UI_TLS_CERT": os.path.join(CERT_DIR, "cert.pem"),
            "SINGBOX_UI_TLS_KEY": os.path.join(CERT_DIR, "key.pem"),
            "SINGBOX_UI_AUTO_SYNC_MINUTES": str(auto_sync_min),
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
            if auto_sync_min == 0:
                # Manual scenarios must not race the auto debouncer:
                # with auto-sync on, a panel edit would auto-push
                # within 5s/30s and the manual sync under test would
                # see no_change instead of applied (or a skipped
                # row). S24 (auto_sync_min=1) keeps it enabled.
                self.api("PATCH", "/api/settings",
                         {"autoSyncEnabled": False})
        except Exception:
            # Never leak the subprocess when construction fails.
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
        # Readiness is the HTTP status only: / serves HTML, not JSON,
        # so a body parse must never decide readiness.
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

    # -- domain helpers -------------------------------------------------
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

    def set_host(self, host):
        return self.api("PATCH", "/api/settings", {"publicHost": host})

    def get_settings(self):
        return self.api("GET", "/api/settings")

    def sync(self):
        return self.api("POST", "/api/sync", {})

    def sync_log(self, limit=50):
        return self.api("GET", f"/api/sync/log?limit={limit}")

    def changes_for(self, sync_id):
        rows = self.api("GET", "/api/sync/changes?limit=200")
        return [r for r in rows if r["syncLogId"] == sync_id]

    def inbounds(self):
        # GET /api/inbounds wraps rows in {"items": [...], ...}.
        return self.api("GET", "/api/inbounds")["items"]

    def subs(self):
        return self.api("GET", "/api/subscriptions")

    def inbound_named(self, name):
        for ib in self.inbounds():
            if ib["name"] == name:
                return ib
        return None

    def sub_named(self, name):
        for s in self.subs():
            if s["name"] == name:
                return s
        return None


def read_target():
    with open(TARGET) as f:
        return json.load(f)


def write_target(doc):
    with open(TARGET, "w") as f:
        json.dump(doc, f, indent=2)


def write_raw_target(text):
    with open(TARGET, "w") as f:
        f.write(text)


def edit_target(fn):
    doc = read_target()
    fn(doc)
    write_target(doc)
    return doc


def target_inbound(doc, tag):
    for ib in doc.get("inbounds", []):
        if ib.get("tag") == tag:
            return ib
    return None


def vless_params(sni, priv=REAL_PRIV, pub=REAL_PUB, short=REAL_SHORT,
                  flow="xtls-rprx-vision"):
    return {"sni": sni, "privateKey": priv, "publicKey": pub,
            "shortId": short, "flow": flow}


def change(rows, action, item_sub):
    return [r for r in rows
            if r["action"] == action and item_sub in r["item"]]


def converge(p, scen):
    """First sync on a fresh instance (missing file -> applied)."""
    row = p.sync()
    check(scen, "converge sync applied", row["result"] == "applied",
          f"got {row['result']}: {row.get('detail')}")
    return row

# ---------------------------------------------------------------- scenarios

def s0_panel_only(p):
    v = p.mk_inbound("V0", "vless-reality", 21100, vless_params("s0.example.com"))
    t = p.mk_template("T0", [v["id"]])
    p.mk_sub("SUB0", t["id"])
    converge(p, "S0")
    cur = p.inbound_named("V0")
    p.api("PATCH", f"/api/inbounds/{v['id']}",
          {"name": "V0", "port": 21101, "params": cur["params"]})
    row = p.sync()
    check("S0", "panel-side change syncs applied", row["result"] == "applied", str(row))
    check("S0", "target port updated", target_inbound(read_target(), "V0")["listen_port"] == 21101)
    check("S0", "panel-side change leaves no change rows",
          p.changes_for(row["id"]) == [], str(p.changes_for(row["id"])))


def s1_passthrough(p):
    v = p.mk_inbound("V1", "vless-reality", 21200, vless_params("s1.example.com"))
    t = p.mk_template("T1", [v["id"]])
    p.mk_sub("SUB1", t["id"])
    converge(p, "S1")

    def edit(doc):
        doc["route"] = {"rules": [{"action": "route", "domain": ["example.com"]}], "final": "direct"}
        doc["dns"] = {"servers": [{"tag": "remote", "address": "8.8.8.8"}]}
        doc["log"]["level"] = "debug"
    edit_target(edit)
    row = p.sync()
    check("S1", "passthrough sync applied (core catches up) (merged already equals file)", row["result"] == "applied", str(row))
    doc = read_target()
    check("S1", "route preserved", doc.get("route", {}).get("final") == "direct")
    check("S1", "dns preserved", bool(doc.get("dns", {}).get("servers")))
    check("S1", "log.level=debug preserved", doc["log"]["level"] == "debug")
    rows = p.changes_for(row["id"])
    for item in ("route", "dns", "log.level"):
        check("S1", f"passthrough row {item}",
              bool(change(rows, "passthrough", item)), str(rows))
    with open(os.path.join(DATA, "logs", "config-changes.log")) as f:
        logtxt = f.read()
    # Guard: the panel has no log-level setting. If one is ever
    # added, classifyLog's level branch must flip from
    # passthrough to revert in the same change — this check
    # fails until then.
    settings = p.api("GET", "/api/settings")
    check("S1", "no log-level panel setting (guard)",
          "logLevel" not in settings and "log_level" not in settings,
          str(sorted(settings.keys())))
    check("S1", "config-changes.log has route line", "route" in logtxt)


def s2_formatting(p):
    v = p.mk_inbound("V2", "vless-reality", 21300, vless_params("s2.example.com"))
    t = p.mk_template("T2", [v["id"]])
    p.mk_sub("SUB2", t["id"])
    converge(p, "S2")
    doc = read_target()
    with open(TARGET, "w") as f:
        json.dump(doc, f, indent=4, sort_keys=True)
    with open(TARGET, "rb") as f:
        reformatted = f.read()
    row = p.sync()
    check("S2", "formatting-only sync is no_change", row["result"] == "no_change", str(row))
    with open(TARGET, "rb") as f:
        after = f.read()
    check("S2", "file bytes untouched (no rewrite, no restart)",
          after == reformatted)
    check("S2", "no change rows on no_change",
          p.changes_for(row["id"]) == [])


def s3_imports(p):
    v = p.mk_inbound("V3", "vless-reality", 21400, vless_params("a.example.com"))
    t = p.mk_template("T3", [v["id"]])
    p.mk_sub("SUB3", t["id"])
    converge(p, "S3")

    def edit(doc):
        ib = target_inbound(doc, "V3")
        ib["listen_port"] = 21401
        ib["tls"]["server_name"] = "b.example.com"
    edit_target(edit)
    row = p.sync()
    check("S3", "panel-wins sync applied", row["result"] == "applied", str(row))
    cur = p.inbound_named("V3")
    check("S3", "panel port kept", cur["port"] == 21400, str(cur))
    check("S3", "panel sni kept", cur["params"]["sni"] == "a.example.com", str(cur["params"]))
    rows = p.changes_for(row["id"])
    pr = change(rows, "reverted", "inbounds[V3].listen_port")
    check("S3", "port reverted row old->new",
          len(pr) == 1 and pr[0]["oldValue"] == "21400" and pr[0]["newValue"] == "21401", str(pr))
    sr = change(rows, "reverted", "inbounds[V3].sni")
    check("S3", "sni reverted row old->new",
          len(sr) == 1 and sr[0]["oldValue"] == "a.example.com" and sr[0]["newValue"] == "b.example.com", str(sr))
    doc = read_target()
    check("S3", "target reverted to panel values",
          target_inbound(doc, "V3")["listen_port"] == 21400
          and target_inbound(doc, "V3")["tls"]["server_name"] == "a.example.com")


def s4a_hy2_uniform(p):
    v = p.mk_inbound("H4a", "hysteria2", 21500,
                     {"password": "pw-old", "obfs": "", "obfsPassword": "",
                      "upMbps": 0, "downMbps": 0})
    t = p.mk_template("T4a", [v["id"]])
    p.mk_sub("SUB4a1", t["id"])
    p.mk_sub("SUB4a2", t["id"])
    converge(p, "S4a")

    def edit(doc):
        for u in target_inbound(doc, "H4a")["users"]:
            u["password"] = "pw-new"
    edit_target(edit)
    row = p.sync()
    check("S4a", "uniform password sync applied", row["result"] == "applied", str(row))
    check("S4a", "panel password kept",
          p.inbound_named("H4a")["params"]["password"] == "pw-old")
    rows = p.changes_for(row["id"])
    pr = change(rows, "reverted", "inbounds[H4a].password")
    check("S4a", "password reverted row hides values",
          len(pr) == 1 and pr[0]["oldValue"] == "" and pr[0]["newValue"] == "", str(pr))
    check("S4a", "target users all panel password",
          all(u["password"] == "pw-old"
              for u in target_inbound(read_target(), "H4a")["users"]))


def s4b_hy2_divergent(p):
    v = p.mk_inbound("H4b", "hysteria2", 21600,
                     {"password": "pw-old", "obfs": "", "obfsPassword": "",
                      "upMbps": 0, "downMbps": 0})
    t = p.mk_template("T4b", [v["id"]])
    p.mk_sub("SUB4b1", t["id"])
    p.mk_sub("SUB4b2", t["id"])
    converge(p, "S4b")

    def edit(doc):
        for u in target_inbound(doc, "H4b")["users"]:
            if u["name"] == "SUB4b1":
                u["password"] = "pw-1"
    edit_target(edit)
    row = p.sync()
    check("S4b", "divergent sync applied", row["result"] == "applied", str(row))
    check("S4b", "panel password kept",
          p.inbound_named("H4b")["params"]["password"] == "pw-old")
    users = {u["name"]: u["password"]
             for u in target_inbound(read_target(), "H4b")["users"]}
    check("S4b", "divergent override dropped, panel password for all",
          users.get("SUB4b1") == "pw-old" and users.get("SUB4b2") == "pw-old", str(users))
    check("S4b", "divergent reverted row",
          bool(change(p.changes_for(row["id"]), "reverted", "inbounds[H4b].users")))


def s5a_uuid(p):
    v = p.mk_inbound("V5", "vless-reality", 21700, vless_params("s5.example.com"))
    t = p.mk_template("T5", [v["id"]])
    sub = p.mk_sub("SUB5", t["id"])
    converge(p, "S5a")
    new_uuid = str(uuid.uuid4())

    def edit(doc):
        target_inbound(doc, "V5")["users"][0]["uuid"] = new_uuid
    edit_target(edit)
    row = p.sync()
    check("S5a", "uuid sync applied", row["result"] == "applied", str(row))
    check("S5a", "panel uuid kept", p.sub_named("SUB5")["uuid"] == sub["uuid"])
    rows = change(p.changes_for(row["id"]), "reverted", "inbounds[V5].users[SUB5].uuid")
    check("S5a", "uuid reverted row hides values",
          len(rows) == 1 and rows[0]["oldValue"] == "" and rows[0]["newValue"] == "", str(rows))
    check("S5a", "target uuid reverted to panel uuid",
          target_inbound(read_target(), "V5")["users"][0]["uuid"] == sub["uuid"])


def s5b_rename(p):
    v = p.mk_inbound("V5b", "vless-reality", 21800, vless_params("s5b.example.com"))
    t = p.mk_template("T5b", [v["id"]])
    p.mk_sub("SUB5b", t["id"])
    converge(p, "S5b")

    def edit(doc):
        target_inbound(doc, "V5b")["users"][0]["name"] = "SUB5b-new"
    edit_target(edit)
    row = p.sync()
    check("S5b", "rename sync applied", row["result"] == "applied", str(row))
    check("S5b", "panel sub name kept", p.sub_named("SUB5b") is not None)
    check("S5b", "file name not adopted", p.sub_named("SUB5b-new") is None)
    rows = change(p.changes_for(row["id"]), "reverted", "inbounds[V5b].users[SUB5b-new].name")
    check("S5b", "rename reverted row old->new",
          len(rows) == 1 and rows[0]["oldValue"] == "SUB5b"
          and rows[0]["newValue"] == "SUB5b-new", str(rows))
    check("S5b", "target user name reverted",
          target_inbound(read_target(), "V5b")["users"][0]["name"] == "SUB5b")


def s6a_new_trojan(p):
    v = p.mk_inbound("V6", "vless-reality", 21900, vless_params("s6.example.com"))
    t = p.mk_template("T6", [v["id"]])
    p.mk_sub("SUB6", t["id"])
    converge(p, "S6a")

    def edit(doc):
        doc["inbounds"].append({
            "tag": "NEW6", "listen": "::", "listen_port": 21901,
            "type": "trojan",
            "users": [{"name": "someone", "password": "trojan-pw"}],
            "tls": {"enabled": True, "server_name": "new6.example.com"},
        })
    edit_target(edit)
    row = p.sync()
    check("S6a", "new trojan sync applied", row["result"] == "applied", str(row))
    check("S6a", "no panel row post-baseline (never imported)",
          p.inbound_named("NEW6") is None)
    rows = change(p.changes_for(row["id"]), "passthrough", "inbounds[NEW6]")
    check("S6a", "new-tag passthrough row",
          len(rows) == 1 and "透传保留" in rows[0]["detail"], str(rows))
    users = target_inbound(read_target(), "NEW6")["users"]
    check("S6a", "file user preserved in target",
          any(u["name"] == "someone" for u in users), str(users))


def s6b_unknown_protocol(p):
    v = p.mk_inbound("V6b", "vless-reality", 22000, vless_params("s6b.example.com"))
    t = p.mk_template("T6b", [v["id"]])
    p.mk_sub("SUB6b", t["id"])
    converge(p, "S6b")

    def edit(doc):
        doc["inbounds"].append({
            "tag": "UNK6", "listen": "::", "listen_port": 22001,
            "type": "wireguard", "peers": [],
        })
    edit_target(edit)
    row = p.sync()
    check("S6b", "unknown-protocol sync applied (core catches up) (merged already equals file)", row["result"] == "applied", str(row))
    check("S6b", "no panel row for unknown protocol",
          p.inbound_named("UNK6") is None)
    check("S6b", "unknown inbound preserved whole",
          target_inbound(read_target(), "UNK6") is not None)
    check("S6b", "unknown passthrough row",
          bool(change(p.changes_for(row["id"]), "passthrough", "inbounds[UNK6]")))


def s7_deletion(p):
    v = p.mk_inbound("V7", "vless-reality", 22100, vless_params("s7.example.com"))
    t = p.mk_template("T7", [v["id"]])
    sub = p.mk_sub("SUB7", t["id"])
    converge(p, "S7")

    def edit(doc):
        doc["inbounds"] = [ib for ib in doc["inbounds"] if ib["tag"] != "V7"]
    edit_target(edit)
    row = p.sync()
    check("S7", "deletion sync applied", row["result"] == "applied", str(row))
    check("S7", "panel row kept (file deletion reverted)",
          p.inbound_named("V7") is not None)
    rows = change(p.changes_for(row["id"]), "reverted", "inbounds[V7]")
    check("S7", "deletion reverted row exists", len(rows) == 1, str(rows))
    check("S7", "target restores V7",
          target_inbound(read_target(), "V7") is not None
          and target_inbound(read_target(), "V7")["listen_port"] == 22100)


def s8_user_absence(p):
    v = p.mk_inbound("V8", "vless-reality", 22200, vless_params("s8.example.com"))
    t = p.mk_template("T8", [v["id"]])
    p.mk_sub("SUB8", t["id"])
    converge(p, "S8")

    def edit(doc):
        target_inbound(doc, "V8")["users"] = []
    edit_target(edit)
    row = p.sync()
    check("S8", "absence sync applied", row["result"] == "applied", str(row))
    check("S8", "subscription kept (never deleted from absence)",
          p.sub_named("SUB8") is not None)
    users = target_inbound(read_target(), "V8")["users"]
    check("S8", "user restored in target",
          any(u["name"] == "SUB8" for u in users), str(users))
    check("S8", "restore_note row",
          bool(change(p.changes_for(row["id"]), "restore_note", "inbounds[V8].users[SUB8]")))


def s9_unknown_user(p):
    v = p.mk_inbound("V9", "vless-reality", 22300, vless_params("s9.example.com"))
    t = p.mk_template("T9", [v["id"]])
    p.mk_sub("SUB9", t["id"])
    converge(p, "S9")

    def edit(doc):
        target_inbound(doc, "V9")["users"].append(
            {"name": "STRANGER", "uuid": str(uuid.uuid4()),
             "flow": "xtls-rprx-vision"})
    edit_target(edit)
    row = p.sync()
    check("S9", "unknown-user sync applied (core catches up) (merged already equals file)", row["result"] == "applied", str(row))
    check("S9", "no subscription auto-created",
          p.sub_named("STRANGER") is None)
    users = target_inbound(read_target(), "V9")["users"]
    check("S9", "stranger preserved in target",
          any(u["name"] == "STRANGER" for u in users), str(users))
    check("S9", "unknown-user passthrough row",
          bool(change(p.changes_for(row["id"]), "passthrough", "inbounds[V9].users[STRANGER]")))


def s9b_stranger_panel_name_collision(p):
    v = p.mk_inbound("V9b", "vless-reality", 25620, vless_params("s9b.example.com"))
    t = p.mk_template("T9b", [v["id"]])
    p.mk_sub("SUB9b", t["id"])
    converge(p, "S9b")
    # Panel creates a new sub AFTER the baseline; file adds a
    # same-name user with a different uuid. Baseline never had
    # COLLIDE, so without the fix this would be a stranger
    # passthrough that merge's name dedupe then drops silently.
    p.mk_sub("COLLIDE", t["id"])
    panel_uuid = p.sub_named("COLLIDE")["uuid"]
    file_uuid = str(uuid.uuid4())

    def edit(doc):
        target_inbound(doc, "V9b")["users"].append(
            {"name": "COLLIDE", "uuid": file_uuid,
             "flow": "xtls-rprx-vision"})
    edit_target(edit)
    row = p.sync()
    users = target_inbound(read_target(), "V9b")["users"]
    collide = [u for u in users if u["name"] == "COLLIDE"]
    check("S9b", "panel uuid kept, single COLLIDE",
          len(collide) == 1 and collide[0]["uuid"] == panel_uuid, str(users))
    rows = p.changes_for(row["id"])
    check("S9b", "collision revert row",
          bool(change(rows, "reverted", "inbounds[V9b].users[COLLIDE]")), str(rows))
    check("S9b", "no passthrough row for COLLIDE",
          not change(rows, "passthrough", "inbounds[V9b].users[COLLIDE]"), str(rows))


def s10a_hy2_uniform_host(p):
    p.set_host("panel.example.com")
    a = p.mk_inbound("H10a", "hysteria2", 22400,
                     {"password": "h10-pw", "obfs": "", "obfsPassword": "",
                      "upMbps": 0, "downMbps": 0})
    b = p.mk_inbound("H10b", "hysteria2", 22401,
                     {"password": "h10-pw", "obfs": "", "obfsPassword": "",
                      "upMbps": 0, "downMbps": 0})
    t = p.mk_template("T10a", [a["id"], b["id"]])
    p.mk_sub("SUB10a", t["id"])
    converge(p, "S10a")
    doc = read_target()
    check("S10a", "generated server_names are panel host",
          target_inbound(doc, "H10a")["tls"].get("server_name") == "panel.example.com"
          and target_inbound(doc, "H10b")["tls"].get("server_name") == "panel.example.com")

    def edit(d):
        target_inbound(d, "H10a")["tls"]["server_name"] = "file.example.com"
        target_inbound(d, "H10b")["tls"]["server_name"] = "file.example.com"
    edit_target(edit)
    row = p.sync()
    check("S10a", "uniform host sync applied", row["result"] == "applied", str(row))
    check("S10a", "setting kept (panel wins)",
          p.get_settings()["publicHost"] == "panel.example.com")
    rows = change(p.changes_for(row["id"]), "reverted", "tls.server_name")
    check("S10a", "host reverted rows (both inbounds)",
          len(rows) == 2, str(rows))
    doc = read_target()
    check("S10a", "target server_names reverted to panel host",
          target_inbound(doc, "H10a")["tls"]["server_name"] == "panel.example.com"
          and target_inbound(doc, "H10b")["tls"]["server_name"] == "panel.example.com")


def s10b_hy2_divergent_host(p):
    p.set_host("panel.example.com")
    a = p.mk_inbound("H10c", "hysteria2", 22500,
                     {"password": "h10-pw", "obfs": "", "obfsPassword": "",
                      "upMbps": 0, "downMbps": 0})
    b = p.mk_inbound("H10d", "hysteria2", 22501,
                     {"password": "h10-pw", "obfs": "", "obfsPassword": "",
                      "upMbps": 0, "downMbps": 0})
    t = p.mk_template("T10b", [a["id"], b["id"]])
    p.mk_sub("SUB10b", t["id"])
    converge(p, "S10b")

    def edit(d):
        target_inbound(d, "H10c")["tls"]["server_name"] = "a.example.com"
        target_inbound(d, "H10d")["tls"]["server_name"] = "b.example.com"
    edit_target(edit)
    row = p.sync()
    check("S10b", "divergent host sync applied", row["result"] == "applied", str(row))
    check("S10b", "setting kept",
          p.get_settings()["publicHost"] == "panel.example.com")
    doc = read_target()
    check("S10b", "target server_names reverted to panel host",
          target_inbound(doc, "H10c")["tls"]["server_name"] == "panel.example.com"
          and target_inbound(doc, "H10d")["tls"]["server_name"] == "panel.example.com")
    rows = p.changes_for(row["id"])
    check("S10b", "divergent reverted rows",
          len(change(rows, "reverted", "tls.server_name")) == 2, str(rows))


def s11_invalid_json(p):
    v = p.mk_inbound("V11", "vless-reality", 22600, vless_params("s11.example.com"))
    t = p.mk_template("T11", [v["id"]])
    p.mk_sub("SUB11", t["id"])
    converge(p, "S11")
    write_raw_target("{invalid json")
    row = p.sync()
    check("S11", "invalid file sync applied", row["result"] == "applied", str(row))
    doc = read_target()
    check("S11", "target is valid panel config again",
          target_inbound(doc, "V11") is not None)
    check("S11", "whole-file dropped row",
          bool(change(p.changes_for(row["id"]), "dropped_illegal", "配置文件")))


def s12_duplicate_tag(p):
    v = p.mk_inbound("V12", "vless-reality", 22700, vless_params("s12.example.com"))
    t = p.mk_template("T12", [v["id"]])
    p.mk_sub("SUB12", t["id"])
    converge(p, "S12")

    def edit(doc):
        dup = dict(target_inbound(doc, "V12"))
        dup["listen_port"] = 22701
        doc["inbounds"].append(dup)
    edit_target(edit)
    row = p.sync()
    check("S12", "duplicate sync applied", row["result"] == "applied", str(row))
    check("S12", "panel port kept", p.inbound_named("V12")["port"] == 22700)
    check("S12", "duplicate dropped row",
          bool(change(p.changes_for(row["id"]), "dropped_illegal", "inbounds[V12]")))
    doc = read_target()
    check("S12", "exactly one V12 in target",
          len([ib for ib in doc["inbounds"] if ib["tag"] == "V12"]) == 1
          and target_inbound(doc, "V12")["listen_port"] == 22700)


def s13_bad_entries(p):
    v = p.mk_inbound("V13", "vless-reality", 22800, vless_params("s13.example.com"))
    t = p.mk_template("T13", [v["id"]])
    p.mk_sub("SUB13", t["id"])
    converge(p, "S13")

    def edit(doc):
        doc["inbounds"].append({"listen_port": 22999})
        doc["inbounds"].append({"tag": "NOTYPE13", "listen_port": 22998})
    edit_target(edit)
    row = p.sync()
    check("S13", "bad-entry sync applied", row["result"] == "applied", str(row))
    check("S13", "no panel row for bad entries",
          p.inbound_named("NOTYPE13") is None)
    rows = p.changes_for(row["id"])
    check("S13", "two dropped rows",
          len(change(rows, "dropped_illegal", "inbounds[")) == 2, str(rows))
    doc = read_target()
    ports = [ib.get("listen_port") for ib in doc["inbounds"]]
    check("S13", "bad entries absent from target",
          22999 not in ports and 22998 not in ports, str(ports))

def s14_reserved_port(p):
    v = p.mk_inbound("V14", "vless-reality", 23000, vless_params("s14.example.com"))
    t = p.mk_template("T14", [v["id"]])
    p.mk_sub("SUB14", t["id"])
    converge(p, "S14")

    def edit(doc):
        target_inbound(doc, "V14")["listen_port"] = 2826
    edit_target(edit)
    row = p.sync()
    check("S14", "reserved-port sync applied", row["result"] == "applied", str(row))
    check("S14", "panel port kept", p.inbound_named("V14")["port"] == 23000)
    check("S14", "reserved port reverted row",
          bool(change(p.changes_for(row["id"]), "reverted", "inbounds[V14].listen_port")))
    check("S14", "target port is panel port",
          target_inbound(read_target(), "V14")["listen_port"] == 23000)


def s15a_key_rotation(p):
    v = p.mk_inbound("V15", "vless-reality", 23100, vless_params("s15.example.com"))
    t = p.mk_template("T15", [v["id"]])
    p.mk_sub("SUB15", t["id"])
    converge(p, "S15a")
    new_priv = base64.urlsafe_b64encode(os.urandom(32)).decode().rstrip("=")

    def edit(doc):
        target_inbound(doc, "V15")["tls"]["reality"]["private_key"] = new_priv
    edit_target(edit)
    row = p.sync()
    check("S15a", "rotation sync applied", row["result"] == "applied", str(row))
    check("S15a", "panel private key kept",
          p.inbound_named("V15")["params"]["privateKey"] == REAL_PRIV)
    check("S15a", "rotation reverted row hides key",
          bool(change(p.changes_for(row["id"]), "reverted", "inbounds[V15].privateKey")))
    check("S15a", "target key is panel key",
          target_inbound(read_target(), "V15")["tls"]["reality"]["private_key"] == REAL_PRIV)


def s15b_invalid_key(p):
    v = p.mk_inbound("V15b", "vless-reality", 23200, vless_params("s15b.example.com"))
    t = p.mk_template("T15b", [v["id"]])
    p.mk_sub("SUB15b", t["id"])
    converge(p, "S15b")

    def edit(doc):
        target_inbound(doc, "V15b")["tls"]["reality"]["private_key"] = "not-a-key!!"
    edit_target(edit)
    row = p.sync()
    check("S15b", "invalid-key sync applied", row["result"] == "applied", str(row))
    check("S15b", "panel key kept",
          p.inbound_named("V15b")["params"]["privateKey"] == REAL_PRIV)
    check("S15b", "invalid params reverted row",
          bool(change(p.changes_for(row["id"]), "reverted", "inbounds[V15b]")))
    check("S15b", "target key is panel key",
          target_inbound(read_target(), "V15b")["tls"]["reality"]["private_key"] == REAL_PRIV)


def s16_bad_uuid(p):
    v = p.mk_inbound("V16", "vless-reality", 23300, vless_params("s16.example.com"))
    t = p.mk_template("T16", [v["id"]])
    sub = p.mk_sub("SUB16", t["id"])
    converge(p, "S16")

    def edit(doc):
        target_inbound(doc, "V16")["users"][0]["uuid"] = "not-a-uuid"
    edit_target(edit)
    row = p.sync()
    check("S16", "bad-uuid sync applied", row["result"] == "applied", str(row))
    check("S16", "panel uuid kept", p.sub_named("SUB16")["uuid"] == sub["uuid"])
    check("S16", "bad uuid reverted row",
          bool(change(p.changes_for(row["id"]), "reverted", "inbounds[V16].users[SUB16].uuid")))
    check("S16", "target uuid is panel uuid",
          target_inbound(read_target(), "V16")["users"][0]["uuid"] == sub["uuid"])


def s17_ss_method(p):
    v = p.mk_inbound("S17", "shadowsocks", 23400,
                     {"method": "aes-256-gcm", "password": "ss-pw"})
    t = p.mk_template("T17", [v["id"]])
    p.mk_sub("SUB17", t["id"])
    converge(p, "S17")

    def edit(doc):
        target_inbound(doc, "S17")["method"] = "bogus-method"
    edit_target(edit)
    row = p.sync()
    check("S17", "bad-method sync applied", row["result"] == "applied", str(row))
    check("S17", "panel method kept",
          p.inbound_named("S17")["params"]["method"] == "aes-256-gcm")
    check("S17", "bad method reverted row",
          bool(change(p.changes_for(row["id"]), "reverted", "inbounds[S17].method")))
    check("S17", "target method is panel method",
          target_inbound(read_target(), "S17")["method"] == "aes-256-gcm")


def s18_tuic_congestion(p):
    v = p.mk_inbound("T18", "tuic", 23500,
                     {"uuid": str(uuid.uuid4()), "password": "tuic-pw",
                      "congestionControl": "bbr", "sni": "t18.example.com"})
    t = p.mk_template("T18", [v["id"]])
    p.mk_sub("SUB18", t["id"])
    converge(p, "S18")

    def edit(doc):
        target_inbound(doc, "T18")["congestion_control"] = "warp-drive"
    edit_target(edit)
    row = p.sync()
    check("S18", "bad-congestion sync applied", row["result"] == "applied", str(row))
    check("S18", "panel congestion kept",
          p.inbound_named("T18")["params"]["congestionControl"] == "bbr")
    check("S18", "bad congestion reverted row",
          bool(change(p.changes_for(row["id"]), "reverted", "inbounds[T18].congestionControl")))
    check("S18", "target congestion is panel value",
          target_inbound(read_target(), "T18")["congestion_control"] == "bbr")


def s19_type_change(p):
    v = p.mk_inbound("V19", "vless-reality", 23600, vless_params("s19.example.com"))
    t = p.mk_template("T19", [v["id"]])
    p.mk_sub("SUB19", t["id"])
    converge(p, "S19")

    def edit(doc):
        target_inbound(doc, "V19")["type"] = "trojan"
    edit_target(edit)
    row = p.sync()
    check("S19", "type-change sync applied", row["result"] == "applied", str(row))
    check("S19", "panel protocol kept",
          p.inbound_named("V19")["protocol"] == "vless-reality")
    check("S19", "type-change reverted row",
          bool(change(p.changes_for(row["id"]), "reverted", "inbounds[V19]")))
    check("S19", "target type is panel type",
          target_inbound(read_target(), "V19")["type"] == "vless")


def s20a_short_id_list(p):
    v = p.mk_inbound("V20", "vless-reality", 23700, vless_params("s20.example.com"))
    t = p.mk_template("T20", [v["id"]])
    p.mk_sub("SUB20", t["id"])
    converge(p, "S20a")

    def edit(doc):
        target_inbound(doc, "V20")["tls"]["reality"]["short_id"] = ["abcd1234", "eeeeffff"]
    edit_target(edit)
    row = p.sync()
    check("S20a", "short_id-list sync applied", row["result"] == "applied", str(row))
    check("S20a", "panel shortId kept",
          p.inbound_named("V20")["params"]["shortId"] == "abcd1234")
    check("S20a", "list dropped, panel single short_id restored",
          target_inbound(read_target(), "V20")["tls"]["reality"]["short_id"] == ["abcd1234"])
    check("S20a", "short_id reverted row",
          bool(change(p.changes_for(row["id"]), "reverted", "inbounds[V20].tls.reality.short_id")))


def s20b_short_id_removed(p):
    v = p.mk_inbound("V20b", "vless-reality", 23800, vless_params("s20b.example.com"))
    t = p.mk_template("T20b", [v["id"]])
    p.mk_sub("SUB20b", t["id"])
    converge(p, "S20b")

    def edit(doc):
        del target_inbound(doc, "V20b")["tls"]["reality"]["short_id"]
    edit_target(edit)
    row = p.sync()
    check("S20b", "short_id-removed sync applied", row["result"] == "applied", str(row))
    check("S20b", "panel shortId kept (not randomized)",
          p.inbound_named("V20b")["params"]["shortId"] == "abcd1234")
    check("S20b", "target restores short_id",
          target_inbound(read_target(), "V20b")["tls"]["reality"]["short_id"] == ["abcd1234"])
    check("S20b", "removal reverted row",
          bool(change(p.changes_for(row["id"]), "reverted", "inbounds[V20b].tls.reality.short_id")))


def s21_flow_removed(p):
    v = p.mk_inbound("V21", "vless-reality", 23900, vless_params("s21.example.com"))
    t = p.mk_template("T21", [v["id"]])
    p.mk_sub("SUB21", t["id"])
    converge(p, "S21")

    def edit(doc):
        for u in target_inbound(doc, "V21")["users"]:
            u.pop("flow", None)
    edit_target(edit)
    row = p.sync()
    check("S21", "flow-removed sync applied", row["result"] == "applied", str(row))
    check("S21", "panel flow kept (not vision)",
          p.inbound_named("V21")["params"]["flow"] == "xtls-rprx-vision")
    users = target_inbound(read_target(), "V21")["users"]
    check("S21", "target users have flow restored",
          all(u.get("flow") == "xtls-rprx-vision" for u in users), str(users))
    check("S21", "flow removal reverted row",
          bool(change(p.changes_for(row["id"]), "reverted", "inbounds[V21].users.flow")))


def s22_disabled_readded(p):
    v = p.mk_inbound("V22", "vless-reality", 24000, vless_params("s22.example.com"))
    t = p.mk_template("T22", [v["id"]])
    p.mk_sub("SUB22", t["id"])
    converge(p, "S22")
    p.api("PATCH", f"/api/inbounds/{v['id']}/enabled", {"enabled": False})

    def edit(doc):
        target_inbound(doc, "V22")["listen_port"] = 24001
    edit_target(edit)
    row = p.sync()
    check("S22", "disabled re-add sync applied", row["result"] == "applied", str(row))
    cur = p.inbound_named("V22")
    check("S22", "row stays disabled", cur["enabled"] is False, str(cur))
    check("S22", "file port not adopted", cur["port"] == 24000, str(cur))
    rows = p.changes_for(row["id"])
    check("S22", "disabled reverted row",
          bool(change(rows, "reverted", "inbounds[V22]")), str(rows))
    check("S22", "target drops the disabled inbound",
          target_inbound(read_target(), "V22") is None)


def s23_panel_created_conflict(p):
    v = p.mk_inbound("V23", "vless-reality", 24100, vless_params("s23.example.com"))
    t = p.mk_template("T23", [v["id"]])
    p.mk_sub("SUB23", t["id"])
    converge(p, "S23")
    p.mk_inbound("V23B", "vless-reality", 24101, vless_params("v23b.example.com"))

    def edit(doc):
        doc["inbounds"].append({
            "tag": "V23B", "listen": "::", "listen_port": 24102,
            "type": "trojan",
            "users": [{"name": "someone", "password": "x"}],
            "tls": {"enabled": True, "server_name": "v23b.example.com"},
        })
    edit_target(edit)
    row = p.sync()
    check("S23", "conflict sync applied", row["result"] == "applied", str(row))
    cur = p.inbound_named("V23B")
    check("S23", "panel row wins (protocol)", cur["protocol"] == "vless-reality", str(cur))
    check("S23", "panel row wins (port)", cur["port"] == 24101, str(cur))
    check("S23", "conflict reverted row",
          bool(change(p.changes_for(row["id"]), "reverted", "inbounds[V23B]")))
    ib = target_inbound(read_target(), "V23B")
    check("S23", "target is panel version",
          ib["type"] == "vless" and ib["listen_port"] == 24101, str(ib))


def s24_first_import_auto(p):
    # No baseline: the first auto-sync IS the first import (no more
    # ambiguity skip). Panel rows are pending, the file is
    # hand-written and matches the panel row, plus a route segment
    # (passthrough) and one illegal entry (no tag, dropped).
    # The file is written immediately after the panel rows, well
    # inside the 5s quiet window, so the first auto push sees it.
    v = p.mk_inbound("V24", "vless-reality", 24200, vless_params("s24.example.com"))
    t = p.mk_template("T24", [v["id"]])
    sub = p.mk_sub("SUB24", t["id"])
    doc = {
        "log": {"level": "info", "timestamp": True,
                "output": os.path.join(DATA, "logs", "sing-box.log")},
        "experimental": {"clash_api": {"external_controller": "127.0.0.1:9090"}},
        "inbounds": [{
            "tag": "V24", "listen": "::", "listen_port": 24200, "type": "vless",
            "users": [{"name": "SUB24", "uuid": sub["uuid"],
                       "flow": "xtls-rprx-vision"}],
            "tls": {"enabled": True, "server_name": "s24.example.com",
                    "reality": {"enabled": True, "private_key": REAL_PRIV,
                                "handshake": {"server": "s24.example.com",
                                              "server_port": 443},
                                "short_id": [REAL_SHORT]}},
        }],
        "outbounds": [{"tag": "direct", "type": "direct"}],
        "route": {"rules": [], "final": "direct"},
    }
    # Illegal entry (no tag): dropped by classify, so the desired merge
    # differs from the file and auto-sync really has to run.
    doc["inbounds"] = doc["inbounds"] + [{"type": "vless", "listen_port": 29999}]
    write_target(doc)
    auto = None
    for _ in range(8):  # ~40s: debouncer 5s quiet + 5s drift poll
        for r in p.sync_log(20):
            if r["action"] == "auto-sync" and r["result"] == "applied":
                auto = r
                break
        if auto:
            break
        time.sleep(5)
    check("S24", "first auto-sync applied (first import)", auto is not None,
          str(p.sync_log(20)))
    if auto:
        check("S24", "first-import detail noted",
              "first import" in (auto.get("detail") or ""), str(auto))
    check("S24", "first-import backup written",
          os.path.exists(os.path.join(DATA, "first-import-backup.json")))
    check("S24", "route preserved after first import",
          read_target().get("route", {}).get("final") == "direct")
    check("S24", "panel row intact", p.inbound_named("V24") is not None)
    check("S24", "target keeps V24",
          target_inbound(read_target(), "V24") is not None)
    row = p.sync()
    check("S24", "manual sync after import no_change", row["result"] == "no_change", str(row))


def s25_missing_file(p):
    v = p.mk_inbound("V25", "vless-reality", 24300, vless_params("s25.example.com"))
    t = p.mk_template("T25", [v["id"]])
    p.mk_sub("SUB25", t["id"])
    converge(p, "S25")
    os.remove(TARGET)
    row = p.sync()
    check("S25", "missing-file sync applied", row["result"] == "applied", str(row))
    check("S25", "file recreated with panel config",
          target_inbound(read_target(), "V25") is not None)
    check("S25", "missing-file note row",
          bool(change(p.changes_for(row["id"]), "restore_note", "配置文件")))


def s26_check_fallback(p):
    v = p.mk_inbound("V26", "vless-reality", 24400, vless_params("s26.example.com"))
    t = p.mk_template("T26", [v["id"]])
    p.mk_sub("SUB26", t["id"])
    converge(p, "S26")

    def edit(doc):
        doc["route"] = {"comment": "CHECK_FAIL_MARKER", "rules": [],
                        "final": "direct"}
    edit_target(edit)
    row = p.sync()
    check("S26", "check-fallback sync applied (not check_failed)",
          row["result"] == "applied", str(row))
    doc = read_target()
    check("S26", "breaking passthrough dropped from target",
          "route" not in doc)
    check("S26", "panel inbound intact",
          target_inbound(doc, "V26") is not None)
    check("S26", "passthrough-dropped row",
          bool(change(p.changes_for(row["id"]), "dropped_illegal", "透传内容")))


def s27_idempotency(p):
    v = p.mk_inbound("V27", "vless-reality", 24500, vless_params("s27.example.com"))
    t = p.mk_template("T27", [v["id"]])
    p.mk_sub("SUB27", t["id"])
    converge(p, "S27")

    def edit(doc):
        target_inbound(doc, "V27")["listen_port"] = 24501
    edit_target(edit)
    row1 = p.sync()
    check("S27", "first sync applied", row1["result"] == "applied", str(row1))
    rows1 = change(p.changes_for(row1["id"]), "reverted", "inbounds[V27].listen_port")
    check("S27", "exactly one port reverted row on first sync", len(rows1) == 1, str(rows1))
    check("S27", "target port reverted to panel port",
          target_inbound(read_target(), "V27")["listen_port"] == 24500)
    row2 = p.sync()
    check("S27", "second sync no_change", row2["result"] == "no_change", str(row2))
    check("S27", "no duplicate rows on second sync",
          p.changes_for(row2["id"]) == [], str(p.changes_for(row2["id"])))


def s0b_panel_password_kept(p):
    v = p.mk_inbound("H0b", "hysteria2", 25000,
                     {"password": "pw-old", "obfs": "", "obfsPassword": "",
                      "upMbps": 0, "downMbps": 0})
    t = p.mk_template("T0b", [v["id"]])
    p.mk_sub("SUB0b", t["id"])
    converge(p, "S0b")
    cur = p.inbound_named("H0b")
    params = dict(cur["params"])
    params["password"] = "pw-new"
    p.api("PATCH", f"/api/inbounds/{v['id']}",
          {"name": "H0b", "port": 25000, "params": params})
    row = p.sync()
    check("S0b", "panel password kept (not reverted to file)",
          p.inbound_named("H0b")["params"]["password"] == "pw-new")
    check("S0b", "no password import row",
          change(p.changes_for(row["id"]), "updated", "inbounds[H0b].password") == [])
    check("S0b", "file converges to panel password",
          target_inbound(read_target(), "H0b")["users"][0]["password"] == "pw-new")


def s0c_panel_rename_kept(p):
    v = p.mk_inbound("V0c", "vless-reality", 25100, vless_params("s0c.example.com"))
    t = p.mk_template("T0c", [v["id"]])
    sub = p.mk_sub("SUB0c", t["id"])
    converge(p, "S0c")
    p.api("PATCH", f"/api/subscriptions/{sub['id']}",
          {"name": "SUB0c-new", "templateId": t["id"],
           "limitBytes": 0, "expireAt": 0, "maxIps": 0})
    row = p.sync()
    check("S0c", "panel rename kept (not reverted to file)",
          p.sub_named("SUB0c-new") is not None)
    check("S0c", "no rename import row",
          change(p.changes_for(row["id"]), "updated", "users[SUB0c") == [])
    users = target_inbound(read_target(), "V0c")["users"]
    check("S0c", "file converges to panel name",
          any(u["name"] == "SUB0c-new" for u in users), str(users))


def s0d_panel_host_kept(p):
    p.set_host("panel.example.com")
    a = p.mk_inbound("H0d", "hysteria2", 25200,
                     {"password": "h0d-pw", "obfs": "", "obfsPassword": "",
                      "upMbps": 0, "downMbps": 0})
    t = p.mk_template("T0d", [a["id"]])
    p.mk_sub("SUB0d", t["id"])
    converge(p, "S0d")
    p.set_host("panel2.example.com")
    row = p.sync()
    check("S0d", "panel host kept (not reverted to file)",
          p.get_settings()["publicHost"] == "panel2.example.com")
    check("S0d", "no host import row",
          change(p.changes_for(row["id"]), "updated", "settings.publicHost") == [])
    check("S0d", "file converges to panel host",
          target_inbound(read_target(), "H0d")["tls"].get("server_name") == "panel2.example.com")


def s0e_panel_disable_wins(p):
    v = p.mk_inbound("V0e", "vless-reality", 25300, vless_params("s0e.example.com"))
    t = p.mk_template("T0e", [v["id"]])
    p.mk_sub("SUB0e", t["id"])
    converge(p, "S0e")
    p.api("PATCH", f"/api/inbounds/{v['id']}/enabled", {"enabled": False})
    row = p.sync()
    cur = p.inbound_named("V0e")
    check("S0e", "row stays disabled (file copy does not resurrect)",
          cur["enabled"] is False, str(cur))
    check("S0e", "no enable row",
          change(p.changes_for(row["id"]), "updated", "inbounds[V0e].enabled") == [])
    check("S0e", "file drops the disabled inbound",
          target_inbound(read_target(), "V0e") is None)


def s0f_panel_flow_kept(p):
    # Panel-side edit of a mapped param (SNI) must survive: the file still
    # holds the baseline SNI, so there is nothing external to import.
    # (Panel flow "" is not a valid premise: validation refills it with
    # xtls-rprx-vision on every update, so the panel can never hold "".)
    v = p.mk_inbound("V0f", "vless-reality", 25600, vless_params("a.example.com"))
    t = p.mk_template("T0f", [v["id"]])
    p.mk_sub("SUB0f", t["id"])
    converge(p, "S0f")
    cur = p.inbound_named("V0f")
    params = dict(cur["params"])
    params["sni"] = "panel.example.com"
    p.api("PATCH", f"/api/inbounds/{v['id']}",
          {"name": "V0f", "port": 25600, "params": params})
    row = p.sync()
    check("S0f", "panel sni kept (not reverted to file)",
          p.inbound_named("V0f")["params"]["sni"] == "panel.example.com")
    check("S0f", "no sni import row",
          change(p.changes_for(row["id"]), "updated", "inbounds[V0f].tls.server_name") == [])
    check("S0f", "file converges to panel sni",
          target_inbound(read_target(), "V0f")["tls"]["server_name"] == "panel.example.com")


def s0g_steady_state_silent(p):
    v = p.mk_inbound("H0g", "hysteria2", 25500,
                     {"password": "pw-old", "obfs": "", "obfsPassword": "",
                      "upMbps": 0, "downMbps": 0})
    t = p.mk_template("T0g", [v["id"]])
    p.mk_sub("SUB0g1", t["id"])
    p.mk_sub("SUB0g2", t["id"])
    converge(p, "S0g")

    def edit(doc):
        for u in target_inbound(doc, "H0g")["users"]:
            if u["name"] == "SUB0g1":
                u["password"] = "pw-1"
    edit_target(edit)
    row1 = p.sync()
    check("S0g", "first sync applied", row1["result"] == "applied", str(row1))
    check("S0g", "divergent reverted row logged once",
          len(change(p.changes_for(row1["id"]), "reverted", "inbounds[H0g].users")) == 1)
    users = {u["name"]: u["password"]
             for u in target_inbound(read_target(), "H0g")["users"]}
    check("S0g", "target users reverted to panel password",
          users.get("SUB0g1") == "pw-old" and users.get("SUB0g2") == "pw-old", str(users))
    row2 = p.sync()
    check("S0g", "second sync no_change", row2["result"] == "no_change", str(row2))
    check("S0g", "no duplicate rows on steady state",
          p.changes_for(row2["id"]) == [], str(p.changes_for(row2["id"])))


def s0h_simultaneous_panel_wins(p):
    v = p.mk_inbound("V0h", "vless-reality", 25600, vless_params("s0h.example.com"))
    t = p.mk_template("T0h", [v["id"]])
    p.mk_sub("SUB0h", t["id"])
    converge(p, "S0h")
    cur = p.inbound_named("V0h")
    p.api("PATCH", f"/api/inbounds/{v['id']}",
          {"name": "V0h", "port": 25601, "params": cur["params"]})

    def edit(doc):
        target_inbound(doc, "V0h")["listen_port"] = 25602
    edit_target(edit)
    row = p.sync()
    check("S0h", "simultaneous edit: panel wins",
          p.inbound_named("V0h")["port"] == 25601)
    rows = change(p.changes_for(row["id"]), "reverted", "inbounds[V0h].listen_port")
    check("S0h", "row shows file value reverted to panel value",
          len(rows) == 1 and rows[0]["oldValue"] == "25601"
          and rows[0]["newValue"] == "25602", str(rows))
    check("S0h", "target port is panel port",
          target_inbound(read_target(), "V0h")["listen_port"] == 25601)


def s0i_panel_file_different_fields(p):
    v = p.mk_inbound("V0i", "vless-reality", 25610, vless_params("s0i.example.com"))
    t = p.mk_template("T0i", [v["id"]])
    p.mk_sub("SUB0i", t["id"])
    converge(p, "S0i")
    cur = p.inbound_named("V0i")
    # Panel changes A (port); file changes B (sni) — different fields.
    p.api("PATCH", f"/api/inbounds/{v['id']}",
          {"name": "V0i", "port": 25611, "params": cur["params"]})

    def edit(doc):
        target_inbound(doc, "V0i")["tls"]["server_name"] = "evil.example.com"
    edit_target(edit)
    row = p.sync()
    check("S0i", "panel port kept", p.inbound_named("V0i")["port"] == 25611)
    check("S0i", "target port is panel port",
          target_inbound(read_target(), "V0i")["listen_port"] == 25611)
    check("S0i", "target sni is panel sni",
          target_inbound(read_target(), "V0i")["tls"]["server_name"] == "s0i.example.com")
    rows = change(p.changes_for(row["id"]), "reverted", "inbounds[V0i].sni")
    check("S0i", "file sni reverted row",
          len(rows) == 1 and rows[0]["oldValue"] == "s0i.example.com"
          and rows[0]["newValue"] == "evil.example.com", str(rows))
    check("S0i", "no port revert row (panel edit converges silently)",
          len(change(p.changes_for(row["id"]), "reverted", "inbounds[V0i].listen_port")) == 0,
          str(p.changes_for(row["id"])))


def s28_first_import(p):
    # Fresh panel, hand-written file: modelled vless + trojan + SS
    # (tier-2 custom method) are imported once as panel rows;
    # route and an unknown-protocol inbound stay passthrough; the
    # original file is backed up.
    doc = {
        "log": {"level": "info", "timestamp": True,
                "output": os.path.join(DATA, "logs", "sing-box.log")},
        "experimental": {"clash_api": {"external_controller": "127.0.0.1:9090"}},
        "inbounds": [
            {"tag": "V28", "listen": "::", "listen_port": 24600,
             "type": "vless",
             "users": [{"name": "FILEUSER", "uuid": str(uuid.uuid4()),
                        "flow": "xtls-rprx-vision"}],
             "tls": {"enabled": True, "server_name": "s28.example.com",
                     "reality": {"enabled": True, "private_key": REAL_PRIV,
                                 "handshake": {"server": "s28.example.com",
                                               "server_port": 443},
                                 "short_id": [REAL_SHORT]}}},
            {"tag": "T28", "listen": "::", "listen_port": 24601,
             "type": "trojan",
             "users": [{"name": "someone", "password": "trojan-pw"}],
             "tls": {"enabled": True, "server_name": "s28t.example.com"}},
            {"tag": "S28ss", "listen": "::", "listen_port": 24602,
             "type": "shadowsocks", "method": "xchacha20-ietf-poly1305",
             "password": "ss-pw"},
            {"tag": "UNK28", "listen": "::", "listen_port": 24603,
             "type": "wireguard", "peers": []},
        ],
        "outbounds": [{"tag": "direct", "type": "direct"}],
        "route": {"rules": [], "final": "direct"},
    }
    write_target(doc)
    row = p.sync()
    check("S28", "first-import sync applied", row["result"] == "applied", str(row))
    v28 = p.inbound_named("V28")
    check("S28", "vless row imported", v28 is not None
          and v28["port"] == 24600
          and v28["params"]["sni"] == "s28.example.com", str(v28))
    t28 = p.inbound_named("T28")
    check("S28", "trojan row imported with file password", t28 is not None
          and t28["params"]["password"] == "trojan-pw", str(t28))
    s28 = p.inbound_named("S28ss")
    check("S28", "SS row imported with custom method verbatim", s28 is not None
          and s28["params"]["method"] == "xchacha20-ietf-poly1305", str(s28))
    check("S28", "no row for unknown protocol", p.inbound_named("UNK28") is None)
    rows = p.changes_for(row["id"])
    for tag in ("V28", "T28", "S28ss"):
        check("S28", f"added row {tag}",
              bool(change(rows, "added", f"inbounds[{tag}]")), str(rows))
    check("S28", "route passthrough row",
          bool(change(rows, "passthrough", "route")), str(rows))
    check("S28", "unknown passthrough row",
          bool(change(rows, "passthrough", "inbounds[UNK28]")), str(rows))
    check("S28", "first-import backup written",
          os.path.exists(os.path.join(DATA, "first-import-backup.json")))
    doc = read_target()
    check("S28", "route preserved", doc.get("route", {}).get("final") == "direct")
    check("S28", "unknown preserved whole",
          target_inbound(doc, "UNK28") is not None)
    check("S28", "file user preserved",
          any(u["name"] == "FILEUSER"
              for u in target_inbound(doc, "V28")["users"]))
    check("S28", "SS method generated verbatim",
          target_inbound(doc, "S28ss")["method"] == "xchacha20-ietf-poly1305")


def s29_extra_field_dropped(p):
    v = p.mk_inbound("V29", "vless-reality", 24700, vless_params("s29.example.com"))
    t = p.mk_template("T29", [v["id"]])
    p.mk_sub("SUB29", t["id"])
    converge(p, "S29")

    def edit(doc):
        target_inbound(doc, "V29")["tls"]["reality"]["max_time_difference"] = 60
    edit_target(edit)
    row = p.sync()
    check("S29", "extra-field sync applied", row["result"] == "applied", str(row))
    check("S29", "extra field reverted row",
          bool(change(p.changes_for(row["id"]), "reverted",
                       "inbounds[V29].tls.reality.max_time_difference")))
    check("S29", "extra field dropped from target",
          "max_time_difference" not in
          target_inbound(read_target(), "V29")["tls"]["reality"])


def s30_ss_custom_method_api(p):
    # Tier 2: a real protocol value outside the fixed dropdown is
    # stored verbatim via the API (previously rejected).
    v = p.mk_inbound("S30", "shadowsocks", 24710,
                     {"method": "xchacha20-ietf-poly1305", "password": "ss-pw"})
    t = p.mk_template("T30", [v["id"]])
    p.mk_sub("SUB30", t["id"])
    check("S30", "custom method accepted by API",
          p.inbound_named("S30")["params"]["method"] == "xchacha20-ietf-poly1305")
    converge(p, "S30")
    check("S30", "custom method stored verbatim",
          p.inbound_named("S30")["params"]["method"] == "xchacha20-ietf-poly1305")
    check("S30", "custom method generated verbatim",
          target_inbound(read_target(), "S30")["method"] == "xchacha20-ietf-poly1305")


def s31_mixed_revert_preserve(p):
    v = p.mk_inbound("V31", "vless-reality", 24800, vless_params("s31.example.com"))
    t = p.mk_template("T31", [v["id"]])
    p.mk_sub("SUB31", t["id"])
    converge(p, "S31")

    def edit(doc):
        target_inbound(doc, "V31")["listen_port"] = 24801
        doc["route"] = {"rules": [], "final": "direct"}
    edit_target(edit)
    row = p.sync()
    check("S31", "mixed sync applied", row["result"] == "applied", str(row))
    doc = read_target()
    check("S31", "managed port reverted to panel port",
          target_inbound(doc, "V31")["listen_port"] == 24800)
    check("S31", "unmanaged route preserved",
          doc.get("route", {}).get("final") == "direct")
    rows = p.changes_for(row["id"])
    check("S31", "reverted row for port",
          bool(change(rows, "reverted", "inbounds[V31].listen_port")), str(rows))
    check("S31", "passthrough row for route",
          bool(change(rows, "passthrough", "route")), str(rows))


SCENARIOS = [
    ("S0", s0_panel_only, 0), ("S0b", s0b_panel_password_kept, 0),
    ("S0c", s0c_panel_rename_kept, 0), ("S0d", s0d_panel_host_kept, 0),
    ("S0e", s0e_panel_disable_wins, 0), ("S0f", s0f_panel_flow_kept, 0),
    ("S0g", s0g_steady_state_silent, 0), ("S0h", s0h_simultaneous_panel_wins, 0), ("S0i", s0i_panel_file_different_fields, 0),
    ("S1", s1_passthrough, 0),
    ("S2", s2_formatting, 0), ("S3", s3_imports, 0),
    ("S4a", s4a_hy2_uniform, 0), ("S4b", s4b_hy2_divergent, 0),
    ("S5a", s5a_uuid, 0), ("S5b", s5b_rename, 0),
    ("S6a", s6a_new_trojan, 0), ("S6b", s6b_unknown_protocol, 0),
    ("S7", s7_deletion, 0), ("S8", s8_user_absence, 0),
    ("S9", s9_unknown_user, 0), ("S9b", s9b_stranger_panel_name_collision, 0), ("S10a", s10a_hy2_uniform_host, 0),
    ("S10b", s10b_hy2_divergent_host, 0), ("S11", s11_invalid_json, 0),
    ("S12", s12_duplicate_tag, 0), ("S13", s13_bad_entries, 0),
    ("S14", s14_reserved_port, 0), ("S15a", s15a_key_rotation, 0),
    ("S15b", s15b_invalid_key, 0), ("S16", s16_bad_uuid, 0),
    ("S17", s17_ss_method, 0), ("S18", s18_tuic_congestion, 0),
    ("S19", s19_type_change, 0), ("S20a", s20a_short_id_list, 0),
    ("S20b", s20b_short_id_removed, 0), ("S21", s21_flow_removed, 0),
    ("S22", s22_disabled_readded, 0), ("S23", s23_panel_created_conflict, 0),
    ("S24", s24_first_import_auto, 1), ("S25", s25_missing_file, 0),
    ("S26", s26_check_fallback, 0), ("S27", s27_idempotency, 0),
    ("S28", s28_first_import, 0), ("S29", s29_extra_field_dropped, 0),
    ("S30", s30_ss_custom_method_api, 0), ("S31", s31_mixed_revert_preserve, 0),
]


def main():
    ensure_fakebin()
    assert os.path.exists(BIN), f"binary missing: {BIN} (run ./build.sh first)"
    wanted = sys.argv[1:]
    todo = [s for s in SCENARIOS if not wanted or s[0] in wanted]
    if not todo:
        print("no matching scenarios")
        return 2
    for name, fn, auto_min in todo:
        print(f"== {name} ==")
        panel = None
        try:
            panel = Panel(auto_sync_min=auto_min)
            fn(panel)
        except Exception as e:
            import traceback
            check(name, "scenario ran without harness error", False,
                  f"{e}\n{traceback.format_exc(limit=3)}")
        finally:
            if panel:
                panel.stop()
    failed = [r for r in RESULTS if not r[2]]
    print(f"\n{len(RESULTS) - len(failed)}/{len(RESULTS)} checks passed, "
          f"{len(failed)} failed")
    for scen, name, _, detail in failed:
        print(f"  FAILED {scen}: {name} -- {detail[:300]}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())

