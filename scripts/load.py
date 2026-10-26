"""Controlled stress and sustained load; count completed turns, not webhook acknowledgements."""
import argparse
import base64
import concurrent.futures
import hashlib
import hmac
import json
import math
import os
import platform
import secrets
import time
import urllib.request
import uuid
from pathlib import Path

import psycopg
from cryptography.hazmat.primitives.ciphers.aead import AESGCM

ROOT = Path(__file__).resolve().parents[1]


def stage(name, concurrency, duration, operation, p95_limit):
    deadline = time.monotonic() + duration
    started = time.monotonic()

    def worker():
        samples, errors = [], []
        while time.monotonic() < deadline:
            start = time.monotonic()
            try:
                operation()
                samples.append(time.monotonic() - start)
            except Exception as exc:
                errors.append(f"{type(exc).__name__}: {exc}")
        return samples, errors

    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
        results = list(pool.map(lambda _: worker(), range(concurrency)))
    samples = sorted(value for values, _ in results for value in values)
    errors = [value for _, values in results for value in values]
    elapsed = time.monotonic() - started
    p95 = samples[math.ceil(len(samples) * .95) - 1] if samples else None
    result = {"name": name, "concurrency": concurrency, "planned_seconds": duration,
              "elapsed_seconds": elapsed, "completed": len(samples), "errors": len(errors),
              "error_examples": errors[:5], "throughput_per_second": len(samples) / elapsed,
              "p95_seconds": p95, "p95_limit_seconds": p95_limit,
              "passed": bool(samples) and not errors and p95 <= p95_limit}
    print(json.dumps(result), flush=True)
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--stage-seconds", type=float, default=5)
    parser.add_argument("--sustained-seconds", type=float, default=60)
    args = parser.parse_args()
    if min(args.stage_seconds, args.sustained_seconds) <= 0:
        parser.error("durations must be positive")
    dsn = os.environ["SONIA_TEST_DATABASE_URL"]
    core = os.environ["SONIA_SMOKE_CORE_URL"]
    secret = secrets.token_hex(32)
    nonce = secrets.token_bytes(12)
    key = base64.b64decode(os.environ["SONIA_SMOKE_ENCRYPTION_KEY"], validate=True)
    sealed = b"\x01" + nonce + AESGCM(key).encrypt(nonce, secret.encode(), b"\x01")
    with psycopg.connect(dsn, autocommit=True) as conn:
        old = conn.execute("SELECT app_secret_encrypted FROM inboxes WHERE phone_number_id = '000000000000000'").fetchone()
        if old is None:
            raise RuntimeError("demo inbox is missing")
        conn.execute("UPDATE inboxes SET app_secret_encrypted = %s WHERE phone_number_id = '000000000000000'", (sealed,))

    def health():
        with urllib.request.urlopen(core + "/health", timeout=5) as response:
            if response.status != 200 or json.load(response) != {"status": "ok"}:
                raise RuntimeError("health response differs from contract")

    def turn():
        token = uuid.uuid4()
        phone = "549351" + str(token.int % 10**9).zfill(9)
        wamid = "wamid.load." + str(token)
        value = {"metadata": {"phone_number_id": "000000000000000"},
                 "contacts": [{"wa_id": phone, "profile": {"name": "Load Test"}}],
                 "messages": [{"from": phone, "id": wamid, "timestamp": str(int(time.time())),
                               "type": "text", "text": {"body": "Hola, el depto de 2 ambientes en Jesus Maria sigue disponible?"}}]}
        body = json.dumps({"object": "whatsapp_business_account", "entry": [{"id": "000000000000000",
                          "changes": [{"field": "messages", "value": value}]}]}).encode()
        signature = hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
        request = urllib.request.Request(core + "/webhooks/whatsapp", body,
                  {"Content-Type": "application/json", "X-Hub-Signature-256": "sha256=" + signature})
        with urllib.request.urlopen(request, timeout=5) as response:
            if response.status != 200:
                raise RuntimeError("webhook was not accepted")
        deadline = time.monotonic() + 10
        with psycopg.connect(dsn, autocommit=True, connect_timeout=5) as conn:
            while time.monotonic() < deadline:
                row = conn.execute("""SELECT b.processed_at, b.error_message, g.status, o.status
                    FROM messages i JOIN message_batches b ON b.id = i.message_batch_id
                    LEFT JOIN generations g ON g.message_batch_id = b.id
                    LEFT JOIN messages o ON o.id = g.message_id WHERE i.wamid = %s""", (wamid,)).fetchone()
                if row and row[1]:
                    raise RuntimeError("turn failed: " + row[1])
                if row and row[0] and row[2:] == ("auto_sent", "sent"):
                    return
                time.sleep(.05)
        raise TimeoutError("no completed and sent generation within 10 seconds")

    results = []
    try:
        for concurrency in (5, 10, 20):
            results.append(stage("health-stress", concurrency, args.stage_seconds, health, 1))
        for concurrency in (1, 3, 5):
            results.append(stage("turn-stress", concurrency, args.stage_seconds, turn, 5))
        results.append(stage("turn-sustained", 3, args.sustained_seconds, turn, 5))
    finally:
        with psycopg.connect(dsn, autocommit=True) as conn:
            conn.execute("UPDATE inboxes SET app_secret_encrypted = %s WHERE phone_number_id = '000000000000000'", (old[0],))
        report = {"environment": platform.platform(), "llm_and_channel_doubles": True,
                  "scope": "Core, Brain, real PostgreSQL; model quality and real provider latency are excluded",
                  "stages": results, "passed": len(results) == 7 and all(r["passed"] for r in results)}
        target = ROOT / "reports/load.json"
        target.parent.mkdir(exist_ok=True)
        target.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
