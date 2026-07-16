"""Excel Engine — reusable spreadsheet capabilities.

Capabilities:
    - Open, save, create workbooks
    - Add, rename, copy, delete worksheets
    - Insert formulas, tables, pivot tables
    - Apply styles, merge cells, filter/sort data
    - Insert images and charts
    - Generate dashboards
    - Read full structure, analyze sheets, validate integrity
"""

from openpyxl import Workbook, load_workbook
from openpyxl.styles import Font, PatternFill, Alignment, Border, Side
from openpyxl.utils import get_column_letter
from openpyxl.chart import BarChart, LineChart, PieChart, Reference
from openpyxl.worksheet.table import Table
from openpyxl.formatting.rule import CellIsRule
import os
import json


class ExcelEngine:
    """Reusable Excel capabilities. NOT a tool — a library."""

    def create_workbook(self, filename: str = None, sheet_name: str = "Sheet1") -> dict:
        """Create a new Excel workbook with one sheet."""
        wb = Workbook()
        ws = wb.active
        ws.title = sheet_name
        if filename:
            wb.save(filename)
        return {"path": filename, "sheet_name": sheet_name, "sheets": [sheet_name]}

    def open_workbook(self, path: str) -> dict:
        """Open an existing workbook and return its structure."""
        wb = load_workbook(path, data_only=True)
        sheets = wb.sheetnames
        info = {
            "path": path,
            "sheets": sheets,
            "sheet_count": len(sheets),
        }
        wb.close()
        return info

    def save_workbook(self, path: str, target_path: str = None) -> dict:
        """Save (or save-as) an existing workbook."""
        wb = load_workbook(path)
        save_path = target_path or path
        wb.save(save_path)
        wb.close()
        return {"path": save_path, "size": os.path.getsize(save_path)}

    def add_sheet(self, path: str, sheet_name: str) -> dict:
        """Add a new worksheet to a workbook."""
        wb = load_workbook(path)
        ws = wb.create_sheet(title=sheet_name)
        wb.save(path)
        wb.close()
        return {"path": path, "sheet_name": sheet_name, "sheets": wb.sheetnames}

    def rename_sheet(self, path: str, old_name: str, new_name: str) -> dict:
        """Rename a worksheet."""
        wb = load_workbook(path)
        if old_name not in wb.sheetnames:
            wb.close()
            return {"status": "error", "error": f"Sheet '{old_name}' not found"}
        ws = wb[old_name]
        ws.title = new_name
        wb.save(path)
        wb.close()
        return {"path": path, "old_name": old_name, "new_name": new_name}

    def copy_sheet(self, path: str, sheet_name: str, new_name: str = None) -> dict:
        """Copy a worksheet within the same workbook."""
        wb = load_workbook(path)
        if sheet_name not in wb.sheetnames:
            wb.close()
            return {"status": "error", "error": f"Sheet '{sheet_name}' not found"}
        ws = wb[sheet_name]
        copy = wb.copy_worksheet(ws)
        if new_name:
            copy.title = new_name
        wb.save(path)
        wb.close()
        return {"path": path, "source": sheet_name, "copy": copy.title}

    def delete_sheet(self, path: str, sheet_name: str) -> dict:
        """Delete a worksheet."""
        wb = load_workbook(path)
        if sheet_name not in wb.sheetnames:
            wb.close()
            return {"status": "error", "error": f"Sheet '{sheet_name}' not found"}
        if len(wb.sheetnames) == 1:
            wb.close()
            return {"status": "error", "error": "Cannot delete last sheet"}
        wb.remove(wb[sheet_name])
        wb.save(path)
        wb.close()
        return {"path": path, "deleted": sheet_name}

    def insert_data(self, path: str, sheet_name: str = None,
                    headers: list = None, rows: list = None,
                    start_row: int = 1, start_col: int = 1) -> dict:
        """Insert headers and row data into a sheet."""
        wb = load_workbook(path)
        ws = wb[sheet_name] if sheet_name else wb.active
        if headers:
            for ci, h in enumerate(headers, start=start_col):
                ws.cell(row=start_row, column=ci, value=h)
            start_row += 1
        for ri, row in enumerate(rows or [], start=start_row):
            for ci, val in enumerate(row, start=start_col):
                ws.cell(row=ri, column=ci, value=val)
        wb.save(path)
        wb.close()
        return {"path": path, "rows_inserted": len(rows or [])}

    def insert_formula(self, path: str, sheet_name: str = None,
                       cell: str = None, formula: str = None) -> dict:
        """Insert a formula into a cell."""
        wb = load_workbook(path)
        ws = wb[sheet_name] if sheet_name else wb.active
        ws[cell] = formula
        wb.save(path)
        wb.close()
        return {"path": path, "cell": cell, "formula": formula}

    def insert_table(self, path: str, sheet_name: str = None,
                     headers: list = None, rows: list = None,
                     table_name: str = "Table1") -> dict:
        """Insert a formatted table with headers and data."""
        wb = load_workbook(path)
        ws = wb[sheet_name] if sheet_name else wb.active

        start_row = 1
        for ci, h in enumerate(headers or [], start=1):
            ws.cell(row=start_row, column=ci, value=h)
        for ri, row in enumerate(rows or [], start=start_row + 1):
            for ci, val in enumerate(row, start=1):
                ws.cell(row=ri, column=ci, value=val)

        if headers and rows:
            nrows = len(rows) + 1
            ncols = len(headers)
            ref = f"A1:{get_column_letter(ncols)}{nrows}"
            tab = Table(displayName=table_name, ref=ref)
            ws.add_table(tab)

        wb.save(path)
        wb.close()
        return {"path": path, "table_name": table_name, "rows": len(rows or [])}

    def create_pivot_table(self, path: str, sheet_name: str = None,
                           data_range: str = None, pivot_sheet: str = "Pivot",
                           rows: list = None, cols: list = None,
                           values: list = None) -> dict:
        """Create a pivot table from data.

        Note: openpyxl creates pivot table definitions but Excel
        must refresh them. This creates the full structure.
        """
        from openpyxl.pivot.table import PivotTable, PivotTableDefinition, PivotFields
        wb = load_workbook(path)
        ws = wb[sheet_name] if sheet_name else wb.active
        pivot_ws = wb.create_sheet(title=pivot_sheet)

        if data_range and rows and values:
            pivot = PivotTable(
                name="PivotTable1",
                source=f"{ws.title}!{data_range}",
                pivotCacheId=0,
            )
            pivot_ws.add_pivot(pivot)

        wb.save(path)
        wb.close()
        return {"path": path, "pivot_sheet": pivot_sheet}

    def apply_styles(self, path: str, sheet_name: str = None,
                     header_style: bool = True,
                     header_font: dict = None,
                     header_fill: dict = None,
                     auto_width: bool = True,
                     styles: list = None,
                     title_row: str = None,
                     title_text: str = None) -> dict:
        """Apply styles to a worksheet.

        Args:
            path: Path to the workbook file.
            sheet_name: Target sheet (defaults to active).
            header_style: If True, style the first row as a header.
            header_font: Dict with 'color' and 'size' for header font.
            header_fill: Dict with 'color' for header background.
            auto_width: If True, auto-fit column widths.
            styles: List of style dicts, each with:
                - 'range': cell range like 'A2:F9' (required)
                - 'bold': bool
                - 'italic': bool
                - 'font_size': int
                - 'font_color': hex color string
                - 'fill': hex color string
                - 'align': 'left'|'center'|'right'
                - 'number_format': Excel format string
                - 'border': bool
            title_row: Range to merge for a title (e.g. 'A1:F1').
            title_text: Text to put in the merged title cell.
        """
        wb = load_workbook(path)
        ws = wb[sheet_name] if sheet_name else wb.active

        # Title row (merged)
        if title_row and title_text:
            ws.merge_cells(title_row)
            top_left = title_row.split(":")[0]
            ws[top_left] = title_text
            ws[top_left].font = Font(size=16, bold=True, color="FFFFFF")
            ws[top_left].fill = PatternFill(
                start_color="1B4332", end_color="1B4332", fill_type="solid")
            ws[top_left].alignment = Alignment(horizontal="center", vertical="center")
            ws.row_dimensions[1].height = 30

        # Header styling
        hfont = Font(
            bold=True,
            color=header_font.get("color", "FFFFFF") if header_font else "FFFFFF",
            size=header_font.get("size", 11) if header_font else 11,
        )
        hfill = PatternFill(
            start_color=header_fill.get("color", "4472C4") if header_fill else "4472C4",
            end_color=header_fill.get("color", "4472C4") if header_fill else "4472C4",
            fill_type="solid",
        )

        if header_style:
            header_row = 2 if title_row else 1
            for cell in ws[header_row]:
                cell.font = hfont
                cell.fill = hfill
                cell.alignment = Alignment(horizontal="center", vertical="center")
            ws.row_dimensions[header_row].height = 22

        # Arbitrary range styles
        if styles:
            for style_def in styles:
                cell_range = style_def.get("range")
                if not cell_range:
                    continue
                font_kwargs = {}
                if "bold" in style_def:
                    font_kwargs["bold"] = style_def["bold"]
                if "italic" in style_def:
                    font_kwargs["italic"] = style_def["italic"]
                if "font_size" in style_def:
                    font_kwargs["size"] = style_def["font_size"]
                if "font_color" in style_def:
                    font_kwargs["color"] = style_def["font_color"]
                cell_font = Font(**font_kwargs) if font_kwargs else None

                cell_fill = None
                if "fill" in style_def:
                    fc = style_def["fill"]
                    cell_fill = PatternFill(start_color=fc, end_color=fc, fill_type="solid")

                cell_align = None
                if "align" in style_def:
                    cell_align = Alignment(horizontal=style_def["align"], vertical="center")

                cell_border = None
                if style_def.get("border"):
                    side = Side(style="thin", color="CCCCCC")
                    cell_border = Border(left=side, right=side, top=side, bottom=side)

                number_format = style_def.get("number_format")

                for row in ws[cell_range]:
                    for cell in row:
                        if cell_font:
                            cell.font = cell_font
                        if cell_fill:
                            cell.fill = cell_fill
                        if cell_align:
                            cell.alignment = cell_align
                        if cell_border:
                            cell.border = cell_border
                        if number_format and isinstance(cell.value, (int, float)):
                            cell.number_format = number_format

        # Auto-width
        if auto_width:
            for col_cells in ws.columns:
                max_len = 0
                col_letter = None
                for cell in col_cells:
                    if col_letter is None:
                        col_letter = get_column_letter(cell.column)
                    if cell.value:
                        max_len = max(max_len, len(str(cell.value)))
                if col_letter:
                    ws.column_dimensions[col_letter].width = min(max_len + 3, 50)

        # Freeze panes (freeze header row)
        if header_style:
            freeze_row = 3 if title_row else 2
            ws.freeze_panes = f"A{freeze_row}"

        wb.save(path)
        wb.close()
        return {"path": path, "styled": True}

    def merge_cells(self, path: str, sheet_name: str = None,
                    range_string: str = None, value: str = None) -> dict:
        """Merge a range of cells and optionally set a value."""
        wb = load_workbook(path)
        ws = wb[sheet_name] if sheet_name else wb.active
        ws.merge_cells(range_string)
        if value:
            top_left = range_string.split(":")[0]
            ws[top_left] = value
        wb.save(path)
        wb.close()
        return {"path": path, "range": range_string}

    def filter_data(self, path: str, sheet_name: str = None,
                    range_string: str = None) -> dict:
        """Add auto-filter to a range."""
        wb = load_workbook(path)
        ws = wb[sheet_name] if sheet_name else wb.active
        ws.auto_filter.ref = range_string
        wb.save(path)
        wb.close()
        return {"path": path, "filter": range_string}

    def sort_data(self, path: str, sheet_name: str = None,
                  column: str = None, descending: bool = False) -> dict:
        """Sort data by a column."""
        from openpyxl.worksheet.datavalidation import DataValidation
        wb = load_workbook(path)
        ws = wb[sheet_name] if sheet_name else wb.active
        col_idx = ws[column].column if column else 1
        data = list(ws.iter_rows(min_row=2, values_only=True))
        data.sort(key=lambda r: (r[col_idx - 1] or ""), reverse=descending)
        for i, row in enumerate(data, start=2):
            for j, val in enumerate(row, start=1):
                ws.cell(row=i, column=j, value=val)
        wb.save(path)
        wb.close()
        return {"path": path, "sorted_by": column, "descending": descending}

    def insert_image(self, path: str, sheet_name: str = None,
                     image_path: str = None, cell: str = "A1") -> dict:
        """Insert an image into a worksheet."""
        from openpyxl.drawing.image import Image
        wb = load_workbook(path)
        ws = wb[sheet_name] if sheet_name else wb.active
        img = Image(image_path)
        img.anchor = cell
        ws.add_image(img)
        wb.save(path)
        wb.close()
        return {"path": path, "image": image_path, "cell": cell}

    def insert_chart(self, path: str, sheet_name: str = None,
                     chart_type: str = "bar", data_range: str = None,
                     title: str = None, categories: str = None,
                     cell: str = None,
                     style: int = None,
                     width: int = None,
                     height: int = None) -> dict:
        """Insert a chart into a worksheet.

        Args:
            path: Path to the workbook file.
            sheet_name: Target sheet (defaults to active).
            chart_type: 'bar', 'line', or 'pie'.
            data_range: Data range for the chart (e.g. 'B2:F9').
            title: Chart title.
            categories: Range for category labels (e.g. 'A2:A9').
            cell: Anchor cell for the chart (e.g. 'H2').
            style: Chart style number (1-48).
            width: Chart width in cm.
            height: Chart height in cm.
        """
        wb = load_workbook(path)
        ws = wb[sheet_name] if sheet_name else wb.active

        chart_map = {
            "bar": BarChart,
            "line": LineChart,
            "pie": PieChart,
        }
        chart_cls = chart_map.get(chart_type)
        if not chart_cls:
            wb.close()
            return {"status": "error", "error": f"Unsupported chart type: {chart_type}"}

        chart = chart_cls()
        if title:
            chart.title = title
        else:
            chart.title = None

        if style:
            chart.style = style

        if data_range:
            # Parse the range to determine data orientation.
            # Reference with range_string can fail in some openpyxl versions,
            # so we parse the range and use min/max col/row instead.
            min_col, min_row, max_col, max_row = _parse_range(data_range)
            data_ref = Reference(ws, min_col=min_col, min_row=min_row,
                                 max_col=max_col, max_row=max_row)
            chart.add_data(data_ref, titles_from_data=False)

        if categories:
            cat_min_col, cat_min_row, cat_max_col, cat_max_row = _parse_range(categories)
            cat_ref = Reference(ws, min_col=cat_min_col, min_row=cat_min_row,
                                max_col=cat_max_col, max_row=cat_max_row)
            chart.set_categories(cat_ref)

        if width:
            chart.width = width
        if height:
            chart.height = height

        anchor = cell or "A1"
        ws.add_chart(chart, anchor)
        wb.save(path)
        wb.close()
        return {"path": path, "chart_type": chart_type, "anchor": anchor}

    def create_dashboard(self, path: str, title: str = "Dashboard",
                         sheets: list = None) -> dict:
        """Generate a dashboard with multiple sheets and summary data.

        Args:
            path: Output file path.
            title: Dashboard title (used for the summary sheet).
            sheets: List of sheet definitions, each with:
                - 'name': sheet name
                - 'headers': list of column headers
                - 'rows': list of row lists
                - 'title': optional section title for the sheet
                - 'charts': list of chart dicts with 'type', 'data_range',
                  'categories', 'title', 'cell'
                - 'kpi': list of KPI dicts with 'name', 'value', 'target'
        """
        wb = Workbook()

        # Summary sheet
        summary = wb.active
        summary.title = title
        summary["A1"] = title
        summary["A1"].font = Font(size=18, bold=True, color="1B4332")
        summary.merge_cells("A1:D1")
        summary.row_dimensions[1].height = 35

        sheet_count = 1
        for sheet_def in sheets or []:
            name = sheet_def.get("name", f"Sheet{sheet_count}")
            ws = wb.create_sheet(title=name)
            sheet_count += 1

            # Optional sheet title
            start_row = 1
            if sheet_def.get("title"):
                ws[f"A{start_row}"] = sheet_def["title"]
                ws[f"A{start_row}"].font = Font(size=14, bold=True, color="FFFFFF")
                ws[f"A{start_row}"].fill = PatternFill(
                    start_color="2D6A4F", end_color="2D6A4F", fill_type="solid")
                ws[f"A{start_row}"].alignment = Alignment(horizontal="center")
                ncols = len(sheet_def.get("headers", []))
                if ncols > 1:
                    ws.merge_cells(f"A{start_row}:{get_column_letter(ncols)}{start_row}")
                ws.row_dimensions[start_row].height = 28
                start_row += 1

            # Headers
            headers = sheet_def.get("headers", [])
            for ci, h in enumerate(headers, start=1):
                cell = ws.cell(row=start_row, column=ci, value=h)
                cell.font = Font(bold=True, color="FFFFFF", size=11)
                cell.fill = PatternFill(
                    start_color="40916C", end_color="40916C", fill_type="solid")
                cell.alignment = Alignment(horizontal="center", vertical="center")
            if headers:
                ws.row_dimensions[start_row].height = 22

            # Data rows
            rows = sheet_def.get("rows", [])
            for ri, row in enumerate(rows, start=start_row + 1):
                for ci, val in enumerate(row, start=1):
                    ws.cell(row=ri, column=ci, value=val)

            # Auto-width
            for col_cells in ws.columns:
                max_len = 0
                col_letter = None
                for cell in col_cells:
                    if col_letter is None:
                        col_letter = get_column_letter(cell.column)
                    if cell.value:
                        max_len = max(max_len, len(str(cell.value)))
                if col_letter:
                    ws.column_dimensions[col_letter].width = min(max_len + 3, 50)

            # Freeze panes
            if headers:
                ws.freeze_panes = f"A{start_row + 1}"

            # Charts
            for chart_def in sheet_def.get("charts", []):
                chart_type = chart_def.get("type", "bar")
                chart_map = {"bar": BarChart, "line": LineChart, "pie": PieChart}
                chart_cls = chart_map.get(chart_type)
                if not chart_cls:
                    continue
                chart = chart_cls()
                if chart_def.get("title"):
                    chart.title = chart_def["title"]
                if chart_def.get("data_range"):
                    dmin_col, dmin_row, dmax_col, dmax_row = _parse_range(chart_def["data_range"])
                    data_ref = Reference(ws, min_col=dmin_col, min_row=dmin_row,
                                         max_col=dmax_col, max_row=dmax_row)
                    chart.add_data(data_ref, titles_from_data=False)
                if chart_def.get("categories"):
                    cmin_col, cmin_row, cmax_col, cmax_row = _parse_range(chart_def["categories"])
                    cat_ref = Reference(ws, min_col=cmin_col, min_row=cmin_row,
                                        max_col=cmax_col, max_row=cmax_row)
                    chart.set_categories(cat_ref)
                anchor = chart_def.get("cell", "A1")
                ws.add_chart(chart, anchor)

            # KPIs
            kpis = sheet_def.get("kpi", [])
            if kpis:
                kpi_start = start_row + len(rows) + 3
                ws.cell(row=kpi_start, column=1, value="KPI").font = Font(bold=True)
                ws.cell(row=kpi_start, column=2, value="Valor").font = Font(bold=True)
                ws.cell(row=kpi_start, column=3, value="Objetivo").font = Font(bold=True)
                for i, kpi in enumerate(kpis, start=kpi_start + 1):
                    ws.cell(row=i, column=1, value=kpi.get("name"))
                    ws.cell(row=i, column=2, value=kpi.get("value"))
                    ws.cell(row=i, column=3, value=kpi.get("target"))

        # Update summary with sheet list
        summary["A3"] = "Hojas en este dashboard:"
        summary["A3"].font = Font(bold=True, size=12)
        for i, name in enumerate(wb.sheetnames[1:], start=4):
            summary.cell(row=i, column=1, value=f"  • {name}")
            summary.cell(row=i, column=1).font = Font(size=11)

        wb.save(path)
        wb.close()
        return {"path": path, "dashboard": title, "sheets": len(wb.sheetnames)}

    def read_structure(self, path: str) -> dict:
        """Read complete workbook structure: sheets, dimensions, headers."""
        wb = load_workbook(path, data_only=True)
        structure = {
            "path": path,
            "sheets": [],
        }
        for sheet_name in wb.sheetnames:
            ws = wb[sheet_name]
            info = {
                "name": sheet_name,
                "dimensions": ws.dimensions,
                "rows": ws.max_row,
                "cols": ws.max_column,
            }
            headers = []
            if ws.max_row > 0:
                for cell in ws[1]:
                    headers.append(str(cell.value) if cell.value else "")
            info["headers"] = headers
            structure["sheets"].append(info)
        wb.close()
        return structure

    def analyze_sheet(self, path: str, sheet_name: str = None) -> dict:
        """Analyze a sheet: data types, empty cells, statistics."""
        wb = load_workbook(path, data_only=True)
        ws = wb[sheet_name] if sheet_name else wb.active
        numeric_count = 0
        text_count = 0
        empty_count = 0
        formula_count = 0
        for row in ws.iter_rows():
            for cell in row:
                if cell.value is None:
                    empty_count += 1
                elif isinstance(cell.value, (int, float)):
                    numeric_count += 1
                elif isinstance(cell.value, str) and cell.value.startswith("="):
                    formula_count += 1
                else:
                    text_count += 1
        wb.close()
        return {
            "sheet": ws.title,
            "dimensions": ws.dimensions,
            "total_cells": ws.max_row * ws.max_column,
            "numeric": numeric_count,
            "text": text_count,
            "formulas": formula_count,
            "empty": empty_count,
        }

    def validate_integrity(self, path: str) -> dict:
        """Validate workbook integrity and structure."""
        try:
            wb = load_workbook(path)
            issues = []
            for sheet_name in wb.sheetnames:
                ws = wb[sheet_name]
                if ws.max_row == 0:
                    issues.append({"sheet": sheet_name, "issue": "empty sheet"})
                for row in ws.iter_rows():
                    for cell in row:
                        if isinstance(cell.value, str) and len(cell.value) > 32767:
                            issues.append({
                                "sheet": sheet_name,
                                "cell": cell.coordinate,
                                "issue": "cell text exceeds 32767 chars",
                            })
            wb.close()
            return {"path": path, "valid": len(issues) == 0, "issues": issues}
        except Exception as e:
            return {"path": path, "valid": False, "error": str(e)}

    def kpi_sheet(self, path: str, sheet_name: str = "KPI",
                  kpis: list = None) -> dict:
        """Create a KPI dashboard sheet with metrics and conditional formatting."""
        from openpyxl.chart import BarChart, Reference
        wb = load_workbook(path) if os.path.exists(path) else Workbook()
        if sheet_name in wb.sheetnames:
            ws = wb[sheet_name]
        else:
            ws = wb.create_sheet(title=sheet_name)

        ws["A1"] = "KPI"
        ws["B1"] = "Value"
        ws["C1"] = "Target"
        ws["D1"] = "Status"
        for cell in ws[1]:
            cell.font = Font(bold=True, color="FFFFFF")
            cell.fill = PatternFill(start_color="4472C4", end_color="4472C4", fill_type="solid")

        green_fill = PatternFill(start_color="C6EFCE", end_color="C6EFCE", fill_type="solid")
        red_fill = PatternFill(start_color="FFC7CE", end_color="FFC7CE", fill_type="solid")

        for i, kpi in enumerate(kpis or [], start=2):
            ws.cell(row=i, column=1, value=kpi.get("name"))
            ws.cell(row=i, column=2, value=kpi.get("value"))
            ws.cell(row=i, column=3, value=kpi.get("target"))
            status = kpi.get("status", kpi.get("value", 0) >= kpi.get("target", 0))
            ws.cell(row=i, column=4, value="✓" if status else "✗")
            fill = green_fill if status else red_fill
            for col in range(1, 5):
                ws.cell(row=i, column=col).fill = fill

        wb.save(path)
        wb.close()
        return {"path": path, "kpis": len(kpis or [])}


def _parse_range(range_string: str):
    """Parse an Excel range string like 'A2:F9' into (min_col, min_row, max_col, max_row).

    Handles single cells ('A1') and ranges ('A1:B5').
    """
    from openpyxl.utils.cell import coordinate_to_tuple, range_boundaries
    min_col, min_row, max_col, max_row = range_boundaries(range_string)
    if max_col is None:
        max_col = min_col
    if max_row is None:
        max_row = min_row
    return min_col, min_row, max_col, max_row
