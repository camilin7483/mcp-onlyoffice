"""Result validation for Python Engine operations.

Every operation result must be validated before returning.
Checks: existence, integrity, format, consistency, structure.

Params must be a dict with string keys; results must be a dict
with JSON-serializable values (IPC crosses stdin/stdout as JSON).
"""

import json
import os

# IPC guardrails (mirrored by the Go bridge and runner.py).
MAX_REQUEST_BYTES = 8 * 1024 * 1024    # 8 MiB per request line, enforced at read time
MAX_STRING_PARAM_LEN = 4 * 1024 * 1024  # 4 MiB per individual string param (utf-8 bytes)

# Param keys that carry filesystem locations. Any key containing one of
# these markers (case-insensitive) whose value looks like a path is
# confined to the allowed output dir (see check_path_confinement).
PATH_KEY_MARKERS = ("path", "output", "dest", "file", "filename", "image")


def validate_params(params, capability: str = "") -> dict:
    """Validate IPC params: must be a dict with string keys plus
    type/range checks on common params for the given capability.

    Returns:
        {"valid": bool, "error": str (only when invalid)}
    """
    if not isinstance(params, dict):
        return {"valid": False, "error": f"Params must be a dict, got {type(params).__name__}"}
    for key in params:
        if not isinstance(key, str):
            return {"valid": False, "error": f"Param keys must be strings, got {type(key).__name__}"}
        if not key:
            return {"valid": False, "error": "Param keys must be non-empty"}
    # Explicit per-param type/range rules (apply to any capability using them).
    for key, value in params.items():
        err = _check_param(key, value)
        if err:
            return {"valid": False, "error": f"Param {key!r} for {capability or 'unknown'}: {err}"}
    # Generic size cap: no individual string param (at any nesting level)
    # may exceed MAX_STRING_PARAM_LEN utf-8 bytes.
    err = _check_sizes(params)
    if err:
        return {"valid": False, "error": f"Params for {capability or 'unknown'}: {err}"}
    return {"valid": True}


def _check_sizes(node) -> str | None:
    """Return an error string when a nested string exceeds the size cap."""
    if isinstance(node, str):
        if len(node.encode("utf-8")) > MAX_STRING_PARAM_LEN:
            return f"string param exceeds {MAX_STRING_PARAM_LEN} bytes"
        return None
    if isinstance(node, dict):
        for value in node.values():
            err = _check_sizes(value)
            if err:
                return err
        return None
    if isinstance(node, (list, tuple)):
        for value in node:
            err = _check_sizes(value)
            if err:
                return err
    return None


def output_dir_from_env() -> str:
    """Allowed output dir, confined via MCP_ONLYOFFICE_ENGINE_OUTPUT_DIR.

    Defaults to the current working directory. Always returned resolved.
    """
    return os.path.realpath(os.environ.get("MCP_ONLYOFFICE_ENGINE_OUTPUT_DIR", os.getcwd()))


def _is_path_key(key: str) -> bool:
    lowered = key.lower()
    return any(marker in lowered for marker in PATH_KEY_MARKERS)


def is_within_dir(value: str, allowed: str) -> bool:
    """True when value resolves inside allowed (or equals it)."""
    resolved = os.path.realpath(os.path.join(allowed, value))
    try:
        return os.path.normcase(os.path.commonpath((resolved, allowed))) == os.path.normcase(allowed)
    except ValueError:
        return False


def check_path_confinement(params, allowed: str) -> dict:
    """Reject output path args resolving outside the allowed dir.

    Walks params (including nested dicts/lists); any string value under a
    path-like key must resolve inside allowed.
    Absolute paths that escape and any ".." segment are rejected.

    Returns {"valid": bool, "error": str (only when invalid)}.
    """
    error = _walk_paths(params, allowed)
    if error:
        return {"valid": False, "error": error}
    return {"valid": True}


def _walk_paths(node, allowed: str) -> str | None:
    if isinstance(node, dict):
        for key, value in node.items():
            if isinstance(key, str) and (key == "data_csv" or _is_path_key(key)):
                candidates = value if isinstance(value, (list, tuple)) else (value,)
                for path in candidates:
                    if not isinstance(path, str) or not path:
                        continue
                    if ".." in path.replace("\\", "/").split("/"):
                        return f"Path traversal rejected in param {key!r}: {path!r}"
                    if "\x00" in path:
                        return f"Invalid path in param {key!r}: NUL byte"
                    name = os.path.basename(os.path.realpath(os.path.join(allowed, path))).lower()
                    if name in (".metadata.json", "audit.jsonl", ".mcp-onlyoffice.lock", ".mcp-tool-writes.lock") or (
                        name.startswith(".~lock.") and name.endswith("#")
                    ):
                        return f"Path in param {key!r} refers to an internal control file"
                    if not is_within_dir(path, allowed):
                        return f"Path in param {key!r} is outside the allowed output dir"
            err = _walk_paths(value, allowed)
            if err:
                return err
        return None
    if isinstance(node, (list, tuple)):
        for value in node:
            err = _walk_paths(value, allowed)
            if err:
                return err
    return None


def _check_param(key: str, value) -> str | None:
    """Return an error string when a known param has a bad type/range, else None."""
    if value is None:
        return None  # optional param left at its default; engine applies defaults
    if key in ("path", "filename", "target_path", "output_path", "output_dir",
               "image_path", "chart_path", "excel_path", "template_path"):
        if not isinstance(value, str) or not value or "\x00" in value:
            return "must be a non-empty string path"
    elif key in ("text", "title", "sheet_name", "old_name", "new_name",
                 "target_format", "format", "layout", "theme", "operation",
                 "method", "group_key", "name"):
        if not isinstance(value, str):
            return f"must be a string, got {type(value).__name__}"
    elif key in ("dpi", "bins", "width", "height", "level"):
        if isinstance(value, bool) or not isinstance(value, int) or value <= 0:
            return "must be a positive int"
        if key == "level" and value > 9:
            return "must be 1-9"
        if key == "dpi" and value > 1200:
            return "must be <= 1200"
    elif key == "slide_index":
        if isinstance(value, bool) or not isinstance(value, int) or value < 0:
            return "must be an int >= 0"
    elif key == "degrees":
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            return "must be a number"
    elif key == "width_inches":
        if isinstance(value, bool) or not isinstance(value, (int, float)) or value < 0.5 or value > 12:
            return "must be a number between 0.5 and 12"
    elif key in ("data", "values", "categories", "labels", "paths",
                 "x_data", "y_data", "y_series", "x_predict", "rows",
                 "headers", "slides"):
        if not isinstance(value, list):
            return f"must be a list, got {type(value).__name__}"
        if len(value) > 100000:
            return "list too large (max 100000 items)"
    elif key in ("value", "target", "current", "top", "left"):
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            return "must be a number"
    return None


def validate_result(result: dict) -> dict:
    """Validate an operation result.

    Args:
        result: The result dict from a capability

    Returns:
        Validation dict with valid bool and warnings list
    """
    warnings = []

    if not isinstance(result, dict):
        return {"valid": False, "warnings": [f"Result must be a dict, got {type(result).__name__}"]}

    # IPC-safe: result must survive a JSON round-trip
    try:
        json.dumps(result)
    except (TypeError, ValueError) as e:
        return {"valid": False, "warnings": [f"Result is not JSON-serializable: {e}"]}

    # Reject raw binary payloads that can never cross the JSON IPC
    for key, value in result.items():
        if isinstance(value, (bytes, bytearray)):
            warnings.append(f"Key '{key}' holds raw bytes; use base64 instead")
        elif hasattr(value, "getvalue") and hasattr(value, "read"):
            warnings.append(f"Key '{key}' holds a binary buffer; use base64 instead")

    # Check for file paths and validate they exist
    file_path = result.get("path")
    if file_path and isinstance(file_path, str):
        if not os.path.exists(file_path):
            warnings.append(f"File path does not exist: {file_path}")
        else:
            stat = os.stat(file_path)
            if stat.st_size == 0:
                warnings.append(f"File is empty: {file_path}")

    # Check document ID
    doc_id = result.get("id")
    if doc_id is None:
        for key in ("document_id", "file_id", "sheet_id", "chart_id"):
            if result.get(key):
                doc_id = result.get(key)
                break

    # Check data integrity
    data = result.get("data")
    if data is not None:
        if isinstance(data, (bytes, bytearray)) and len(data) == 0:
            warnings.append("Data is empty")

    # Check size
    size = result.get("size", result.get("length"))
    if size is not None and isinstance(size, (int, float)) and size == 0:
        warnings.append("Zero size reported")

    # Check for required keys based on operation type
    if "error" in result:
        warnings.append(f"Result contains error: {result['error']}")

    return {"valid": len(warnings) == 0, "warnings": warnings}
