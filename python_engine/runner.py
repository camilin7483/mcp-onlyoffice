#!/usr/bin/env python3
"""Python Engine Runner — CLI entry point for Go→Python IPC.

This script receives JSON commands on stdin and returns JSON results on stdout.
It is the bridge between the Go MCP server and the Python Engine capabilities.

Protocol:
    Input:  JSON line with {"capability": "...", "params": {...}}
    Output: JSON line with {"status": "ok"|"error"|"warning", "data": ..., "error": ...}

Usage:
    echo '{"capability": "excel.create_workbook", "params": {"filename": "/tmp/test.xlsx"}}' | python3 runner.py

The runner stays alive across multiple requests (persistent process).
Errors in individual capabilities do NOT kill the process.
"""

import sys
import json
import traceback
import os
import signal

# Ensure the parent directory is on the path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from python_engine import PythonEngine


def main():
    # Ignore SIGPIPE — when the Go side closes stdin, we exit cleanly.
    # If we don't handle this, Python gets killed by SIGPIPE and the Go
    # bridge sees a broken pipe on its next write attempt.
    try:
        signal.signal(signal.SIGPIPE, signal.SIG_DFL)
    except (AttributeError, ValueError):
        pass  # SIGPIPE may not exist on all platforms (e.g., Windows)

    engine = PythonEngine()

    # Use line-buffered stdout for reliable IPC.
    # This ensures each response line is flushed immediately.
    stdout = sys.stdout
    try:
        sys.stdout.reconfigure(line_buffering=True)
    except (AttributeError, RuntimeError):
        pass  # reconfigure not available on all Python versions/builds

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue

        try:
            request = json.loads(line)
        except json.JSONDecodeError as e:
            response = {"status": "error", "error": f"Invalid JSON: {e}"}
            print(json.dumps(response), flush=True)
            continue

        capability = request.get("capability", "")
        params = request.get("params", {})

        if capability == "__list__":
            response = {"status": "ok", "data": engine.list_capabilities()}
        elif capability == "__version__":
            response = {"status": "ok", "data": {"version": "1.0.0", "engine": "python_engine"}}
        else:
            try:
                response = engine.execute(capability, params)
            except Exception as e:
                response = {
                    "status": "error",
                    "error": str(e),
                    "traceback": traceback.format_exc(),
                }

        try:
            print(json.dumps(response), flush=True)
        except BrokenPipeError:
            # The Go side has closed the pipe; exit cleanly.
            break

    stdout.flush()


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        pass
    except BrokenPipeError:
        pass
