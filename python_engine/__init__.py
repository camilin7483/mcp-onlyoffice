"""python_engine — Internal reusable capabilities library for ONLYOFFICE.

This is NOT a tool. This is NOT a script generator.
This is a permanent capability layer invoked by the MCP coordinator.

Architecture:
    Model → Planner → MCP → Python Engine → ONLYOFFICE

The engine provides reusable modules:
    - excel/: advanced spreadsheet operations
    - charts/: chart generation (bar, line, pie, financial, KPI)
    - documents/: advanced document creation and manipulation
    - presentations/: slide deck creation and manipulation
    - analytics/: data analysis, KPIs, statistics, predictions
    - pdf/: PDF generation and manipulation
    - images/: image processing and generation
"""

from python_engine.excel import ExcelEngine
from python_engine.charts import ChartEngine
from python_engine.documents import DocumentEngine
from python_engine.presentations import PresentationEngine
from python_engine.analytics import AnalyticsEngine
from python_engine.pdf import PDFEngine
from python_engine.images import ImageEngine
from python_engine.validator import validate_params, validate_result

import base64


def _sanitize(value):
    """Convert a result to JSON-serializable data for the stdin/stdout IPC.

    Binary buffers (BytesIO from charts) become base64 strings;
    anything else non-serializable falls back to str().
    """
    import json as _json

    if isinstance(value, dict):
        return {k: _sanitize(v) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [_sanitize(v) for v in value]
    if isinstance(value, (bytes, bytearray)):
        return base64.b64encode(bytes(value)).decode()
    if hasattr(value, "getvalue") and callable(value.getvalue):
        try:
            return base64.b64encode(value.getvalue()).decode()
        except Exception:
            return str(value)
    try:
        _json.dumps(value)
        return value
    except (TypeError, ValueError):
        return str(value)


class PythonEngine:
    """Central entry point for all Python Engine capabilities.

    The planner decides which capability to invoke.
    The engine executes and returns structured results.
    """

    def __init__(self):
        self.excel = ExcelEngine()
        self.charts = ChartEngine()
        self.documents = DocumentEngine()
        self.presentations = PresentationEngine()
        self.analytics = AnalyticsEngine()
        self.pdf = PDFEngine()
        self.images = ImageEngine()

    def execute(self, capability: str, params: dict) -> dict:
        """Execute a named capability with parameters.

        Only "module.method" (exactly one dot) is accepted, where module
        is one of the explicit MODULES below and method is a public
        callable defined directly on that engine object. Nested paths
        ("a.b.c"), private/dunder attributes and anything else are rejected.

        Args:
            capability: Dot-notation path to capability (e.g. "excel.create_workbook")
            params: Capability-specific parameters

        Returns:
            Result dict with status, data, and optional error

        Raises:
            ValueError: If capability is unknown
        """
        if not isinstance(capability, str):
            return {"status": "error", "error": f"Invalid capability: {capability!r}"}
        parts = capability.split(".")
        if len(parts) != 2 or not all(parts):
            return {"status": "error", "error": f"Invalid capability: {capability}"}

        module_name, method_name = parts
        # Explicit registry: no blind getattr chains, no nested traversal.
        modules = {
            "excel": self.excel,
            "charts": self.charts,
            "documents": self.documents,
            "presentations": self.presentations,
            "analytics": self.analytics,
            "pdf": self.pdf,
            "images": self.images,
        }
        module = modules.get(module_name)
        if module is None:
            return {"status": "error", "error": f"Unknown module: {module_name}"}
        if method_name.startswith("_"):
            return {"status": "error", "error": f"Unknown capability: {capability}"}

        method = getattr(module, method_name, None)
        if method is None or not callable(method):
            return {"status": "error", "error": f"Unknown capability: {capability}"}

        check = validate_params(params, capability)
        if not check["valid"]:
            return {"status": "error", "error": check["error"]}

        try:
            result = _sanitize(method(**params))
            if not isinstance(result, dict):
                return {"status": "error",
                        "error": f"Capability {capability} must return a dict, got {type(result).__name__}"}
            validation = validate_result(result)
            if not validation["valid"]:
                return {"status": "warning", "data": result, "warnings": validation["warnings"]}
            return {"status": "ok", "data": result}
        except Exception as e:
            return {"status": "error", "error": str(e)}

    def list_capabilities(self) -> list:
        """List all available capabilities with descriptions."""
        caps = []
        for module_name in ["excel", "charts", "documents", "presentations", "analytics", "pdf", "images"]:
            module = getattr(self, module_name)
            for method_name in dir(module):
                if method_name.startswith("_"):
                    continue
                method = getattr(module, method_name)
                if callable(method):
                    doc = (method.__doc__ or "").strip().split("\n")[0]
                    caps.append({
                        "capability": f"{module_name}.{method_name}",
                        "description": doc,
                    })
        return caps
