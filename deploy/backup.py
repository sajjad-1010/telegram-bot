#!/usr/bin/env python3
"""Daily backup of the bot's data, in two copies.

1. Local copy (database only) under backups/, kept out of git. One file per
   tier: daily, weekly (ISO week), monthly and yearly. When a tier's new file
   is saved, the previous file of that tier is deleted, so at most four
   database copies exist on disk.
2. Telegram copy (database + logs) sent to the admin's DM once a day. Sent
   messages are never touched afterwards.

Retries: every Telegram call is retried with backoff. If sending still fails
(no network, Bot API server not up yet after a reboot) the script exits
non-zero and systemd runs it again (Restart=on-failure). The local step is
idempotent, and a state file records the last day sent, so re-runs neither
duplicate local files nor send the same day twice.

Uses only the standard library so it keeps working if the venv breaks.
"""

import datetime
import fcntl
import json
import os
import shutil
import sqlite3
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

TIERS = ["daily", "weekly", "monthly", "yearly"]


def load_env(path):
    env = {}
    try:
        raw = path.read_text(encoding="utf-8-sig")
    except FileNotFoundError:
        return env
    for line in raw.splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        env[key.strip()] = value.strip().strip('"').strip("'")
    return env


ENV = load_env(ROOT / ".env")


def cfg(key, default=""):
    return os.environ.get(key) or ENV.get(key) or default


TOKEN = cfg("BOT_TOKEN")
API = (cfg("BACKUP_API_URL") or cfg("TELEGRAM_LOCAL_API_URL") or "https://api.telegram.org").rstrip("/")
MAX_ATTEMPTS = int(cfg("BACKUP_MAX_ATTEMPTS", "6"))
RETRY_BASE = float(cfg("BACKUP_RETRY_BASE_SECONDS", "5"))
SILENT = cfg("BACKUP_SILENT", "true").lower() == "true"
BACKUP_DIR = Path(cfg("BACKUP_DIR", str(ROOT / "backups")))
STATE_PATH = Path(cfg("BACKUP_STATE_PATH", str(ROOT / "data" / "backup-state.json")))


def backup_chat_id():
    explicit = cfg("BACKUP_CHAT_ID")
    if explicit:
        return int(explicit)
    admins = [a.strip() for a in cfg("ADMIN_USER_ID").split(",") if a.strip()]
    if not admins:
        raise SystemExit("backup: set BACKUP_CHAT_ID or ADMIN_USER_ID")
    return int(admins[0])


def log(msg):
    print(msg, flush=True)


class TelegramError(Exception):
    pass


def encode(fields, files):
    boundary = uuid.uuid4().hex
    out = bytearray()
    for name, value in fields.items():
        out += f"--{boundary}\r\nContent-Disposition: form-data; name=\"{name}\"\r\n\r\n".encode()
        out += str(value).encode() + b"\r\n"
    for name, (filename, data, mime) in files.items():
        out += (
            f"--{boundary}\r\nContent-Disposition: form-data; name=\"{name}\"; "
            f"filename=\"{filename}\"\r\nContent-Type: {mime}\r\n\r\n"
        ).encode()
        out += data + b"\r\n"
    out += f"--{boundary}--\r\n".encode()
    return bytes(out), f"multipart/form-data; boundary={boundary}"


def send_document(fields, files):
    """sendDocument with retries on network errors, 5xx and 429.

    A 4xx answer (bad request, unauthorized) is permanent and raised at once.
    The URL carries the token, so it is never logged."""
    url = f"{API}/bot{TOKEN}/sendDocument"
    body, content_type = encode(fields, files)
    last = "unknown error"
    for attempt in range(1, MAX_ATTEMPTS + 1):
        wait = min(RETRY_BASE * 2 ** (attempt - 1), 120)
        try:
            req = urllib.request.Request(url, data=body, headers={"Content-Type": content_type})
            with urllib.request.urlopen(req, timeout=180) as resp:
                data = json.load(resp)
            if data.get("ok"):
                return data["result"]
            raise TelegramError(data.get("description", "not ok"))
        except urllib.error.HTTPError as e:
            try:
                payload = json.load(e)
            except Exception:
                payload = {}
            last = payload.get("description", f"HTTP {e.code}")
            if e.code == 429:
                wait = payload.get("parameters", {}).get("retry_after", wait)
            elif 400 <= e.code < 500:
                raise TelegramError(last) from None
        except (urllib.error.URLError, OSError) as e:
            last = str(getattr(e, "reason", e))
        if attempt < MAX_ATTEMPTS:
            log(f"sendDocument: attempt {attempt}/{MAX_ATTEMPTS} failed ({last}), retrying in {wait:.0f}s")
            time.sleep(wait)
    raise TelegramError(f"sendDocument failed after {MAX_ATTEMPTS} attempts: {last}")


def period_key(tier, now):
    if tier == "daily":
        return now.strftime("%Y-%m-%d")
    if tier == "weekly":
        year, week, _ = now.isocalendar()
        return f"{year}-W{week:02d}"
    if tier == "monthly":
        return now.strftime("%Y-%m")
    return now.strftime("%Y")


def load_state():
    try:
        return json.loads(STATE_PATH.read_text(encoding="utf-8"))
    except FileNotFoundError:
        return {}


def save_state(state):
    STATE_PATH.parent.mkdir(parents=True, exist_ok=True)
    tmp = STATE_PATH.with_suffix(".tmp")
    tmp.write_text(json.dumps(state, ensure_ascii=False, indent=2), encoding="utf-8")
    os.replace(tmp, STATE_PATH)


def snapshot_db(dest):
    """Consistent copy of the live SQLite database (safe while the bot writes)."""
    db_path = ROOT / cfg("DB_PATH", "data/bot.db")
    if not db_path.exists():
        raise SystemExit(f"backup: database not found at {db_path}")
    src = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
    dst = sqlite3.connect(dest)
    with dst:
        src.backup(dst)
    src.close()
    dst.close()


def save_local(snapshot, now):
    """Keep one database file per tier; a new period's file replaces the old one."""
    for tier in TIERS:
        tier_dir = BACKUP_DIR / tier
        tier_dir.mkdir(parents=True, exist_ok=True)
        target = tier_dir / f"bot-{period_key(tier, now)}.db"
        if not target.exists():
            tmp = target.with_suffix(".tmp")
            shutil.copyfile(snapshot, tmp)
            os.replace(tmp, target)
            log(f"local {tier} backup saved: {target.relative_to(BACKUP_DIR)}")
        # Only after the new file is in place, drop the previous one(s).
        for old in tier_dir.iterdir():
            if old != target:
                old.unlink()
                log(f"local {tier} backup removed: {old.name}")


def send_to_admin(snapshot, workdir, now):
    name = f"stash-backup-{now:%Y-%m-%d_%H%M}.tar.gz"
    archive = Path(workdir) / name
    with tarfile.open(archive, "w:gz") as tar:
        tar.add(snapshot, arcname="bot.db")
        logs = ROOT / "logs"
        if logs.is_dir():
            for f in sorted(logs.iterdir()):
                if f.is_file():
                    tar.add(f, arcname=f"logs/{f.name}")
    data = archive.read_bytes()

    chat_id = backup_chat_id()
    msg = send_document(
        {
            "chat_id": chat_id,
            "caption": f"🗄 بک‌آپ روزانه — {now:%Y-%m-%d %H:%M}\n📦 دیتابیس + لاگ‌ها",
            "disable_notification": "true" if SILENT else "false",
        },
        {"document": (name, data, "application/gzip")},
    )
    if not (msg.get("document") or {}).get("file_id") or msg.get("chat", {}).get("id") != chat_id:
        raise TelegramError("backup was not delivered as a document")
    log(f"verified backup delivered to {chat_id} (message {msg['message_id']}, {len(data)} bytes)")


def run(now):
    state = load_state()
    today = period_key("daily", now)
    with tempfile.TemporaryDirectory(prefix="stash-backup-") as workdir:
        snapshot = Path(workdir) / "bot.db"
        snapshot_db(snapshot)

        # The local copy needs no network, so it is saved even when sending fails.
        save_local(snapshot, now)

        if state.get("last_sent") == today:
            log(f"backup for {today} already sent to admin")
            return 0
        try:
            send_to_admin(snapshot, workdir, now)
        except TelegramError as e:
            log(f"sending to admin failed, will retry: {e}")
            return 1
    state["last_sent"] = today
    save_state(state)
    return 0


def main():
    if not TOKEN:
        raise SystemExit("backup: BOT_TOKEN missing")
    STATE_PATH.parent.mkdir(parents=True, exist_ok=True)
    lock = open(STATE_PATH.with_suffix(".lock"), "w")
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        log("another backup run is in progress")
        return 0
    fake = cfg("BACKUP_FAKE_NOW")
    now = datetime.datetime.fromisoformat(fake) if fake else datetime.datetime.now()
    return run(now)


if __name__ == "__main__":
    sys.exit(main())
