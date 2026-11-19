"""Makes `python_engine/` importable regardless of the working directory.

`test_documents_report.py` does `from documents import DocumentEngine`,
which only resolves when this directory is on `sys.path`. Pytest loads
this conftest before collecting, so both of these work:

- `python3 -m pytest python_engine -q` (from the repo root)
- `python3 -m pytest -q` (from inside `python_engine/`)
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
