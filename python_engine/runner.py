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
from python_engine.validator import (
    MAX_REQUEST_BYTES,
    check_path_confinement,
    output_dir_from_env,
)

# Capability names are exactly "module.method" (two segments), matching the
# Go bridge allowlist. Anything else (nested "a.b.c", paths) is rejected.
MAX_CAPABILITY_SEGMENTS = 2


def _read_request_line(buf, limit=MAX_REQUEST_BYTES):
    """Read one raw request line, capping total size at limit bytes.

    Returns (text, oversized): text is None at clean EOF; oversized is True
    when the line exceeded limit, in which case the remainder of the line
    is drained so the stream stays aligned for the next request.
    """
    chunks = []
    total = 0
    while True:
        part = buf.readline(65536)
        if not part:
            if not chunks:
                return None, False
            return b"".join(chunks).decode("utf-8", "replace"), False
        chunks.append(part)
        total += len(part)
        if total > limit + 1:
            # Drain the rest of the oversized line, then report it.
            while part and not part.endswith(b"\n"):
                part = buf.readline(65536)
                if not part:
                    break
            return "", True
        if part.endswith(b"\n"):
            return b"".join(chunks).decode("utf-8", "replace"), False


def _with_id(response, req_id):
    """Echo the request id back so the Go bridge can correlate answers."""
    if req_id is not None:
        response["id"] = req_id
    return response


def main():
    # Ignore SIGPIPE — when the Go side closes stdin, we exit cleanly.
    # If we don't handle this, Python gets killed by SIGPIPE and the Go
    # bridge sees a broken pipe on its next write attempt.
    try:
        signal.signal(signal.SIGPIPE, signal.SIG_DFL)
    except (AttributeError, ValueError):
        pass  # SIGPIPE may not exist on all platforms (e.g., Windows)

    engine = PythonEngine()

    # All relative paths validated by the engine are resolved against this
    # output dir; chdir once so the capability modules cannot write elsewhere.
    out_dir = output_dir_from_env()
    try:
        os.makedirs(out_dir, exist_ok=True)
        os.chdir(out_dir)
    except OSError as exc:
        sys.stderr.write(f"cannot use output dir {out_dir}: {exc}\n")
        sys.exit(1)

    # Use line-buffered stdout for reliable IPC.
    # This ensures each response line is flushed immediately.
    stdout = sys.stdout
    try:
        sys.stdout.reconfigure(line_buffering=True)
    except (AttributeError, RuntimeError):
        pass  # reconfigure not available on all Python versions/builds

    stdin_buf = sys.stdin.buffer

    while True:
        line, oversized = _read_request_line(stdin_buf)
        if line is None:
            break  # clean EOF
        if oversized:
            response = {"status": "error",
                        "error": f"Request exceeds {MAX_REQUEST_BYTES} bytes, rejected"}
            print(json.dumps(response), flush=True)
            continue
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

        req_id = request.get("id")
        capability = request.get("capability", "")
        params = request.get("params", {})

        if capability == "__list__":
            response = _with_id({"status": "ok", "data": engine.list_capabilities()}, req_id)
        elif capability == "__version__":
            response = _with_id({"status": "ok", "data": {"version": "1.0.0", "engine": "python_engine"}}, req_id)
        elif not isinstance(capability, str) or not capability:
            response = _with_id({"status": "error",
                        "error": "Field 'capability' must be a non-empty string like 'excel.create_workbook'"}, req_id)
        elif not isinstance(params, dict):
            response = _with_id({"status": "error",
                        "error": f"Field 'params' must be a dict, got {type(params).__name__}"}, req_id)
        elif len(capability.split(".")) != MAX_CAPABILITY_SEGMENTS or not all(capability.split(".")):
            response = _with_id({"status": "error",
                        "error": f"Invalid capability: {capability!r} (must be exactly 'module.method')"}, req_id)
        else:
            # Confine every output path argument to the allowed output dir
            # before the engine touches the filesystem.
            confinement = check_path_confinement(params, output_dir_from_env())
            if not confinement["valid"]:
                response = _with_id({"status": "error", "error": confinement["error"]}, req_id)
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
                response = _with_id(response, req_id)

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
