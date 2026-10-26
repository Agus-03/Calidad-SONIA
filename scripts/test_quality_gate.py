"""Guard against false green quality gates and incorrect diff line mapping."""
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from quality_gate import changed_lines, evaluate


class QualityGateTests(unittest.TestCase):
    def evaluate(self, go_hits=1, py_percent=80, duplicate=5, diff=""):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            go, py, dup = (root / name for name in ("go.out", "python.json", "duplication.json"))
            go.write_text("mode: set\ngithub.com/mateo-velasquez/sonia/src/core/internal/a/a.go:10.1,11.2 1 " + str(go_hits) + "\n")
            py.write_text(json.dumps({"files": {"a.py": {"executed_lines": [1, 2, 3, 4], "missing_lines": [5]}},
                                      "totals": {"num_statements": 5, "percent_covered": py_percent}}))
            dup.write_text(json.dumps({"statistics": {"total": {"percentage": duplicate}}}))
            with patch("quality_gate.subprocess.run", return_value=SimpleNamespace(stdout=diff)):
                return evaluate(go, py, dup, "HEAD")

    def test_exact_thresholds_pass(self):
        self.assertTrue(self.evaluate()["passed"])

    def test_existing_coverage_below_threshold_blocks(self):
        self.assertFalse(self.evaluate(py_percent=79.99)["passed"])
        self.assertFalse(self.evaluate(go_hits=0)["passed"])

    def test_duplication_over_five_blocks(self):
        self.assertFalse(self.evaluate(duplicate=5.01)["passed"])

    def test_uncovered_changed_lines_block_even_with_good_global_coverage(self):
        result = self.evaluate(diff="+++ b/src/brain/a.py\n@@ -5 +5 @@\n-old\n+new\n")
        self.assertFalse(result["passed"])
        self.assertEqual(result["python_new_code"]["percent"], 0)

    def test_deleted_lines_do_not_move_new_line_numbers(self):
        diff = "+++ b/src/core/a.go\n@@ -8,2 +10,2 @@\n-old\n-old\n+one\n+two\n"
        self.assertEqual(list(changed_lines(diff)), [("src/core/a.go", 10), ("src/core/a.go", 11)])


if __name__ == "__main__":
    unittest.main()
