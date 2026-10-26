"""One command to deploy the disposable stack, verify it and retain evidence."""
import argparse
import json
import os
import shutil
import subprocess
import sys
import time
import xml.etree.ElementTree as ET
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
REPORTS = ROOT / "reports"
COMPOSE = ["docker", "compose", "-f", "deploy/docker-compose.quality.yml"]
ENV = os.environ.copy()
ENV.update({
    "SONIA_TEST_DATABASE_URL": "postgresql://sonia:sonia_test_password@127.0.0.1:55432/sonia_test",
    "SONIA_TEST_QDRANT_URL": "http://127.0.0.1:56333",
    "SONIA_SMOKE_CORE_URL": "http://127.0.0.1:58080",
    "SONIA_SMOKE_BRAIN_URL": "http://127.0.0.1:58000",
    "SONIA_SMOKE_STORAGE_URL": "http://127.0.0.1:54443",
    "SONIA_SMOKE_QDRANT_URL": "http://127.0.0.1:56333",
    "SONIA_SMOKE_ENCRYPTION_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
    "SONIA_SMOKE_DOCUMENT_ENCRYPTION_KEY": "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=",
    "GOTMPDIR": str(ROOT / ".gotmp"),
})


def run(name, command, cwd=ROOT, data=None):
    print(f"Running {name}", flush=True)
    started = time.monotonic()
    with (REPORTS / f"{name}.txt").open("w", encoding="utf-8") as log:
        proc = subprocess.run(command, cwd=cwd, env=ENV, input=data, text=True,
                              encoding="utf-8", errors="replace", stdout=log, stderr=subprocess.STDOUT)
    steps.append({"name": name, "exit_code": proc.returncode, "seconds": round(time.monotonic() - started, 3)})
    if proc.returncode:
        raise RuntimeError(f"{name} failed; see reports/{name}.txt")


def no_go_skips(name):
    rows = [json.loads(row) for row in (REPORTS / f"{name}.txt").read_text(encoding="utf-8").splitlines() if row.startswith("{")]
    tests = [row for row in rows if row.get("Test")]
    if not tests or any(row["Action"] in {"skip", "fail"} for row in tests):
        raise RuntimeError(f"{name}: empty suite, failure or skipped integration")


steps = []


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", default="HEAD")
    parser.add_argument("--no-build", action="store_true")
    parser.add_argument("--keep-stack", action="store_true")
    args = parser.parse_args()
    REPORTS.mkdir(exist_ok=True)
    for name in ("execution.json", "quality-gate.json", "load.json", "python-coverage.json", "python-coverage.xml", "python-tests.xml"):
        (REPORTS / name).unlink(missing_ok=True)
    (ROOT / ".gotmp").mkdir(exist_ok=True)
    python = ROOT / "src/brain/.venv" / ("Scripts/python.exe" if os.name == "nt" else "bin/python")
    if not python.exists():
        raise SystemExit("Install Brain dependencies with Poetry in src/brain/.venv first")
    try:
        run("gate-regressions", [str(python), "-m", "unittest", "discover", "-s", "scripts", "-p", "test_*.py"])
        run("reset-test-stack", COMPOSE + ["down", "--volumes"])
        run("deploy", COMPOSE + ["up", "-d", "--wait", "--wait-timeout", "180"] + ([] if args.no_build else ["--build"]))
        run("seed", COMPOSE + ["exec", "-T", "db", "psql", "-v", "ON_ERROR_STOP=1", "-U", "sonia", "-d", "sonia_test"],
            data=(ROOT / "src/core/seeds/0001_demo.sql").read_text(encoding="utf-8"))
        core = ROOT / "src/core"
        run("go-format", [str(python), "scripts/check_go_format.py"])
        run("go-dependencies", ["go", "mod", "verify"], core)
        run("go-vet", ["go", "vet", "./..."], core)
        run("go-build", ["go", "build", "./..."], core)
        go = ["go", "test", "-count=1", "-json"]
        if os.environ.get("CI"):
            go.append("-race")
        run("go-tests", go + ["-coverprofile=coverage.out", "./internal/...", "./migrations/..."], core)
        no_go_skips("go-tests")
        run("python-static", [str(python), "-m", "ruff", "check", "--select", "E9,F63,F7,F82", "."], ROOT / "src/brain")
        run("contracts", [str(python), "contracts/generate.py", "--check"])
        run("python-tests", [str(python), "-m", "pytest", "--cov-report=term-missing",
            "--cov-report=json:../../reports/python-coverage.json", "--cov-report=xml:../../reports/python-coverage.xml",
            "--junitxml=../../reports/python-tests.xml"], ROOT / "src/brain")
        suites = ET.parse(REPORTS / "python-tests.xml").getroot().findall("testsuite")
        if not suites or any(int(s.get("skipped", "0")) or int(s.get("tests", "0")) == 0 for s in suites):
            raise RuntimeError("Python suite was empty or skipped integration cases")
        bun = shutil.which("bun")
        if bun:
            run("frontend-install", [bun, "install", "--frozen-lockfile"], ROOT / "src/frontend")
            run("frontend-lint", [bun, "run", "lint"], ROOT / "src/frontend")
            run("frontend-build", [bun, "run", "build"], ROOT / "src/frontend")
        else:
            run("frontend-install", ["npx", "--yes", "bun@1.2.17", "install", "--frozen-lockfile"], ROOT / "src/frontend")
            run("frontend-lint", ["npx", "--yes", "bun@1.2.17", "run", "lint"], ROOT / "src/frontend")
            run("frontend-build", ["npx", "--yes", "bun@1.2.17", "run", "build"], ROOT / "src/frontend")
        run("smoke", ["go", "test", "-count=1", "-json", "./smoke/..."], core)
        no_go_skips("smoke")
        run("load", [str(python), "scripts/load.py"])
        if bun:
            run("duplication", [bun, "x", "jscpd@5.4.2", "--config", ".jscpd.json"])
        else:
            run("duplication", ["npx", "--yes", "jscpd@5.4.2", "--config", ".jscpd.json"])
        run("quality-gate", [str(python), "scripts/quality_gate.py", "--base", args.base])
        print("Quality checks passed. Evidence: reports/", flush=True)
    finally:
        try:
            run("service-logs", COMPOSE + ["logs", "--no-color", "--tail", "200"])
            if not args.keep_stack:
                run("cleanup", COMPOSE + ["down", "--volumes"])
        finally:
            (REPORTS / "execution.json").write_text(json.dumps(steps, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
