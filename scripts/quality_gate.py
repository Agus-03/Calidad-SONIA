"""Read real coverage reports and enforce per-service and changed-code thresholds."""
import argparse
import json
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
GO_PREFIX = "github.com/mateo-velasquez/sonia/"


def go_coverage(path):
    lines, total, covered = {}, 0, 0
    for row in path.read_text(encoding="utf-8-sig").splitlines()[1:]:
        location, statements, hits = row.split()
        filename, span = location.rsplit(":", 1)
        filename = filename.removeprefix(GO_PREFIX)
        if filename.endswith("/enums_gen.go") or filename == "src/core/main.go":
            continue
        start, end = (int(point.split(".")[0]) for point in span.split(","))
        total += int(statements)
        covered += int(statements) if int(hits) else 0
        if int(statements):
            for line in range(start, end + 1):
                key = (filename, line)
                lines[key] = lines.get(key, False) or int(hits) > 0
    if not total:
        raise ValueError("Go coverage is empty")
    return lines, 100 * covered / total


def python_coverage(path):
    report = json.loads(path.read_text(encoding="utf-8"))
    lines = {}
    for filename, data in report["files"].items():
        filename = "src/brain/" + filename.replace("\\", "/")
        for line in data["executed_lines"]:
            lines[filename, line] = True
        for line in data["missing_lines"]:
            lines[filename, line] = False
    if not report["totals"]["num_statements"]:
        raise ValueError("Python coverage is empty")
    return lines, report["totals"]["percent_covered"], report["totals"]


def changed_lines(diff):
    filename, line = None, 0
    for row in diff.splitlines():
        if row.startswith("+++ b/"):
            filename = row[6:]
        elif row.startswith("@@"):
            line = int(re.search(r"\+(\d+)", row)[1])
        elif row.startswith("+") and filename:
            yield filename, line
            line += 1
        elif row.startswith(" "):
            line += 1


def evaluate(go_path, python_path, duplication_path, base):
    go_lines, go_total = go_coverage(go_path)
    py_lines, py_total, py_details = python_coverage(python_path)
    duplicate = json.loads(duplication_path.read_text(encoding="utf-8"))["statistics"]["total"]["percentage"]
    diff = subprocess.run(
        ["git", "diff", "--no-ext-diff", "-w", "--unified=0", base, "--", "src/core", "src/brain"],
        cwd=ROOT, check=True, capture_output=True, text=True, encoding="utf-8",
    ).stdout
    changed = set(changed_lines(diff))
    result = {"go_statement_percent": go_total, "python_line_and_branch_percent": py_total,
              "python_details": py_details, "duplication_percent": duplicate, "base": base}
    errors = []
    for name, value in (("Go", go_total), ("Python", py_total)):
        if value < 80:
            errors.append(f"{name}: coverage {value:.2f}% < 80%")
    if duplicate > 5:
        errors.append(f"Duplication {duplicate:.2f}% > 5%")
    for name, lines in (("go", go_lines), ("python", py_lines)):
        measured = changed.intersection(lines)
        percent = 100 * sum(lines[key] for key in measured) / len(measured) if measured else None
        result[f"{name}_new_code"] = {"lines": len(measured), "percent": percent}
        if percent is not None and percent < 80:
            errors.append(f"{name}: changed executable lines coverage {percent:.2f}% < 80%")
    result["errors"] = errors
    result["passed"] = not errors
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", default="HEAD")
    args = parser.parse_args()
    result = evaluate(ROOT / "src/core/coverage.out", ROOT / "reports/python-coverage.json",
                      ROOT / "reports/duplication/jscpd-report.json", args.base)
    (ROOT / "reports/quality-gate.json").write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2))
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
