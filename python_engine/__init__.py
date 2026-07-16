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
from python_engine.validator import validate_result


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

        Args:
            capability: Dot-notation path to capability (e.g. "excel.create_workbook")
            params: Capability-specific parameters

        Returns:
            Result dict with status, data, and optional error

        Raises:
            ValueError: If capability is unknown
        """
        parts = capability.split(".")
        if len(parts) < 2:
            return {"status": "error", "error": f"Invalid capability: {capability}"}

        module_name = parts[0]
        method_name = ".".join(parts[1:])

        module = getattr(self, module_name, None)
        if module is None:
            return {"status": "error", "error": f"Unknown module: {module_name}"}

        method = _resolve_method(module, method_name)
        if method is None:
            return {"status": "error", "error": f"Unknown capability: {capability}"}

        try:
            result = method(**params)
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


def _resolve_method(module, path: str):
    """Resolve a dot-separated method path on a module."""
    current = module
    for part in path.split("."):
        current = getattr(current, part, None)
        if current is None:
            return None
    return current if callable(current) else None
