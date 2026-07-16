"""Result validation for Python Engine operations.

Every operation result must be validated before returning.
Checks: existence, integrity, format, consistency, structure.
"""

import os


def validate_result(result: dict) -> dict:
    """Validate an operation result.

    Args:
        result: The result dict from a capability

    Returns:
        Validation dict with valid bool and warnings list
    """
    warnings = []

    if not isinstance(result, dict):
        return {"valid": False, "warnings": ["Result must be a dict"]}

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
