"""Confinement + IPC guardrail tests for runner.py / validator.py.

Run from this directory: python3 -m unittest test_confinement
"""
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from python_engine import validator
from python_engine.runner import _read_request_line
from python_engine.validator import (
    check_path_confinement,
    output_dir_from_env,
    validate_params,
)


class TestPathConfinement(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.allowed = os.path.realpath(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def check(self, params):
        return check_path_confinement(params, self.allowed)

    def test_relative_inside_ok(self):
        self.assertTrue(self.check({"path": "report.xlsx"})["valid"])
        self.assertTrue(self.check({"output_dir": "sub/dir"})["valid"])

    def test_parent_traversal_rejected(self):
        r = self.check({"path": "../evil.xlsx"})
        self.assertFalse(r["valid"])
        self.assertIn("traversal", r["error"].lower())

    def test_dotdot_inside_still_rejected(self):
        r = self.check({"output": "sub/../ok.xlsx"})
        self.assertFalse(r["valid"])

    def test_absolute_outside_rejected(self):
        r = self.check({"path": "/etc/passwd"})
        self.assertFalse(r["valid"])
        self.assertIn("outside", r["error"].lower())

    def test_absolute_inside_ok(self):
        inside = os.path.join(self.allowed, "a.xlsx")
        self.assertTrue(self.check({"path": inside})["valid"])

    def test_nested_params_checked(self):
        r = self.check({"opts": {"dest": "../x.xlsx"}})
        self.assertFalse(r["valid"])
        r = self.check({"items": [{"file": "/etc/passwd"}]})
        self.assertFalse(r["valid"])

    def test_non_path_keys_untouched(self):
        self.assertTrue(self.check({"text": "a/b ../c"})["valid"])

    def test_plain_ids_untouched(self):
        self.assertTrue(self.check({"file": "abc123"})["valid"])

    def test_env_var_override(self):
        os.environ["MCP_ONLYOFFICE_ENGINE_OUTPUT_DIR"] = self.allowed
        try:
            self.assertEqual(output_dir_from_env(), self.allowed)
        finally:
            del os.environ["MCP_ONLYOFFICE_ENGINE_OUTPUT_DIR"]


class TestSizeCaps(unittest.TestCase):
    def test_big_string_param_rejected(self):
        big = "x" * (validator.MAX_STRING_PARAM_LEN + 1)
        r = validate_params({"text": big}, "test.echo")
        self.assertFalse(r["valid"])

    def test_read_line_caps(self):
        buf = io.BytesIO(b'{"a": 1}\n')
        text, oversized = _read_request_line(buf, limit=64)
        self.assertFalse(oversized)
        self.assertEqual(json.loads(text)["a"], 1)

    def test_read_line_oversized_drains(self):
        buf = io.BytesIO(b"A" * 100 + b"\n" + b'{"b": 2}\n')
        text, oversized = _read_request_line(buf, limit=10)
        self.assertTrue(oversized)
        text2, oversized2 = _read_request_line(buf, limit=10)
        self.assertFalse(oversized2)
        self.assertEqual(json.loads(text2)["b"], 2)


class TestRunnerEndToEnd(unittest.TestCase):
    def _spawn(self, outdir):
        env = dict(os.environ, MCP_ONLYOFFICE_ENGINE_OUTPUT_DIR=outdir)
        runner = os.path.join(os.path.dirname(os.path.abspath(__file__)), "runner.py")
        return subprocess.Popen(
            [sys.executable, runner],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE,
            stderr=subprocess.PIPE, text=True, cwd=outdir, env=env)

    def _roundtrip(self, proc, payload):
        proc.stdin.write(json.dumps(payload) + "\n")
        proc.stdin.flush()
        return json.loads(proc.stdout.readline())

    def test_id_echo_and_confinement(self):
        with tempfile.TemporaryDirectory() as outdir:
            proc = self._spawn(outdir)
            try:
                r = self._roundtrip(proc, {"id": 7, "capability": "__version__", "params": {}})
                self.assertEqual(r.get("id"), 7)
                self.assertEqual(r.get("status"), "ok")

                r = self._roundtrip(proc, {"id": 8, "capability": "excel.create_workbook",
                                           "params": {"path": "/etc/evil.xlsx"}})
                self.assertEqual(r.get("id"), 8)
                self.assertEqual(r.get("status"), "error")

                r = self._roundtrip(proc, {"id": 9, "capability": "a.b.c", "params": {}})
                self.assertEqual(r.get("id"), 9)
                self.assertEqual(r.get("status"), "error")
            finally:
                proc.stdin.close()
                proc.wait(timeout=10)
                proc.stderr.close()


if __name__ == "__main__":
    unittest.main()
