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
import uuid

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

        if not isinstance(request, dict):
            response = {"status": "error",
                        "error": "Request must be a JSON object with 'capability' and 'params'"}
            print(json.dumps(response), flush=True)
            continue

        capability = request.get("capability", "")
        params = request.get("params", {})

        if capability == "__list__":
            response = {"status": "ok", "data": engine.list_capabilities()}
        elif capability == "__version__":
            response = {"status": "ok", "data": {"version": "1.0.0", "engine": "python_engine"}}
        elif not isinstance(capability, str) or not capability:
            response = {"status": "error",
                        "error": "Field 'capability' must be a non-empty string like 'excel.create_workbook'"}
        elif not isinstance(params, dict):
            response = {"status": "error",
                        "error": f"Field 'params' must be a dict, got {type(params).__name__}"}
        else:
            try:
                response = engine.execute(capability, params)
            except Exception as e:
                # Never leak tracebacks to the client: generic error + ID,
                # full traceback to stderr for operators.
                eid = uuid.uuid4().hex[:8]
                print(f"[engine-error id={eid}] {capability}: {traceback.format_exc()}",
                      file=sys.stderr, flush=True)
                response = {
                    "status": "error",
                    "error": f"Internal engine error (id={eid})",
                }

        if not isinstance(response, dict):
            response = {"status": "error",
                        "error": f"Engine returned non-dict response of type {type(response).__name__}"}
        elif response.get("status") not in ("ok", "error", "warning"):
            response = {"status": "error",
                        "error": f"Engine returned invalid status: {response.get('status')!r}"}

        try:
            print(json.dumps(response), flush=True)
        except (TypeError, ValueError) as e:
            fallback = {"status": "error",
                        "error": f"Response is not JSON-serializable: {e}"}
            print(json.dumps(fallback), flush=True)
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
