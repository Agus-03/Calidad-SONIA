"""Check gofmt while allowing Git's CRLF checkout on Windows."""
import subprocess
from pathlib import Path

root = Path(__file__).resolve().parents[1] / "src/core"
result = subprocess.run(["gofmt", "-l", "."], cwd=root, check=True, capture_output=True, text=True)
bad = []
for filename in result.stdout.splitlines():
    source = (root / filename).read_text(encoding="utf-8")
    formatted = subprocess.run(["gofmt"], input=source, check=True, capture_output=True, text=True, encoding="utf-8").stdout
    if source != formatted:
        bad.append(filename)
if bad:
    print("\n".join(bad))
    raise SystemExit(1)
print("All Go files match gofmt (line endings normalized)")
