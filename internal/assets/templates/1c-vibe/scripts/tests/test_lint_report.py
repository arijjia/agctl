import importlib.util
from pathlib import Path
import unittest

CHECKER_PATH = (
    Path(__file__).resolve().parents[1]
    / "demo-project"
    / "ci"
    / "check_lint.py"
)
SPEC = importlib.util.spec_from_file_location("check_lint", CHECKER_PATH)
check_lint = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(check_lint)


class LintReportTest(unittest.TestCase):
    def test_clean_report_without_diagnostics_passes(self):
        report = {"fileinfos": [{"path": "Module.bsl", "diagnostics": []}]}
        self.assertEqual(check_lint.check_report(report), (0, 0))

    def test_error_is_counted(self):
        report = {
            "fileinfos": [
                {
                    "path": "Module.bsl",
                    "diagnostics": [
                        {"severity": "Information"},
                        {"severity": "Error"},
                        {"severity": 1},
                    ],
                }
            ]
        }
        self.assertEqual(check_lint.check_report(report), (3, 2))

    def test_unknown_report_shape_fails_closed(self):
        with self.assertRaises(ValueError):
            check_lint.check_report({"unexpected": []})


if __name__ == "__main__":
    unittest.main()
