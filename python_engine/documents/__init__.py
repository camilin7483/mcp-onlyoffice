"""Document Engine — reusable document capabilities.

Capabilities:
    - Create, edit, analyze documents
    - Insert tables, images, indexes, headers, footers
    - Create automated reports
    - Extract content and structure
    - Template-based generation
"""

from docx import Document
from docx.shared import Inches, Pt, RGBColor
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.enum.table import WD_TABLE_ALIGNMENT
from docx.enum.section import WD_ORIENT
from docx.oxml.ns import qn
import os


class DocumentEngine:
    """Reusable document capabilities. NOT a tool — a library."""

    def create_document(self, path: str = None) -> dict:
        """Create a new empty document."""
        doc = Document()
        if path:
            doc.save(path)
        return {"path": path} if path else {"created": True}

    def open_document(self, path: str) -> dict:
        """Open an existing document and return structure info."""
        doc = Document(path)
        info = {
            "path": path,
            "paragraphs": len(doc.paragraphs),
            "tables": len(doc.tables),
            "sections": len(doc.sections),
            "size": os.path.getsize(path),
        }
        return info

    def add_heading(self, path: str, text: str, level: int = 1) -> dict:
        """Add a heading to the document."""
        doc = Document(path)
        doc.add_heading(text, level=level)
        doc.save(path)
        return {"path": path, "heading": text, "level": level}

    def add_paragraph(self, path: str, text: str,
                      bold: bool = False, italic: bool = False,
                      size: int = 11, alignment: str = "left",
                      color: str = None) -> dict:
        """Add a paragraph with formatting."""
        align_map = {
            "left": WD_ALIGN_PARAGRAPH.LEFT,
            "center": WD_ALIGN_PARAGRAPH.CENTER,
            "right": WD_ALIGN_PARAGRAPH.RIGHT,
            "justify": WD_ALIGN_PARAGRAPH.JUSTIFY,
        }
        doc = Document(path)
        para = doc.add_paragraph()
        para.alignment = align_map.get(alignment, WD_ALIGN_PARAGRAPH.LEFT)
        run = para.add_run(text)
        run.bold = bold
        run.italic = italic
        run.font.size = Pt(size)
        if color:
            run.font.color.rgb = RGBColor(*self._parse_color(color))
        doc.save(path)
        return {"path": path, "paragraphs": len(doc.paragraphs)}

    def insert_table(self, path: str, headers: list = None,
                     rows: list = None, style: str = "Table Grid") -> dict:
        """Insert a table with headers and data."""
        doc = Document(path)
        ncols = len(headers) if headers else (len(rows[0]) if rows else 1)
        nrows = (1 if headers else 0) + len(rows or [])
        table = doc.add_table(rows=nrows, cols=ncols)
        table.style = style
        if headers:
            for i, h in enumerate(headers):
                table.rows[0].cells[i].text = str(h)
        for ri, row in enumerate(rows or [], start=1 if headers else 0):
            for ci, val in enumerate(row):
                table.rows[ri].cells[ci].text = str(val)
        doc.save(path)
        stats = {"path": path, "rows": nrows, "cols": ncols}
        return stats

    def insert_image(self, path: str, image_path: str,
                     width: float = None, add_paragraph: bool = True) -> dict:
        """Insert an image into the document."""
        doc = Document(path)
        if add_paragraph and len(doc.paragraphs) > 0:
            doc.add_paragraph()
        if width:
            doc.add_picture(image_path, width=Inches(width))
        else:
            doc.add_picture(image_path)
        doc.save(path)
        return {"path": path, "image": image_path}

    def add_header(self, path: str, text: str) -> dict:
        """Add text to the document header."""
        doc = Document(path)
        section = doc.sections[0]
        header = section.header
        para = header.paragraphs[0]
        para.text = text
        para.alignment = WD_ALIGN_PARAGRAPH.CENTER
        doc.save(path)
        return {"path": path, "header": text}

    def add_footer(self, path: str, text: str) -> dict:
        """Add text to the document footer."""
        doc = Document(path)
        section = doc.sections[0]
        footer = section.footer
        para = footer.paragraphs[0]
        para.text = text
        para.alignment = WD_ALIGN_PARAGRAPH.CENTER
        doc.save(path)
        return {"path": path, "footer": text}

    def add_page_break(self, path: str) -> dict:
        """Add a page break."""
        doc = Document(path)
        doc.add_page_break()
        doc.save(path)
        return {"path": path}

    def create_report(self, path: str, title: str = "Report",
                      sections: list = None, author: str = None) -> dict:
        """Create a structured report with multiple sections."""
        doc = Document()
        doc.add_heading(title, 0)
        if author:
            para = doc.add_paragraph(f"Prepared by: {author}")

        for section in sections or []:
            stype = section.get("type", "text")
            heading = section.get("heading", "")
            content = section.get("content", "")
            if heading:
                doc.add_heading(heading, level=1)
            if stype == "text":
                for line in content.split("\n"):
                    if line.strip():
                        doc.add_paragraph(line)
            elif stype == "table":
                headers = section.get("headers", [])
                rows = section.get("rows", [])
                ncols = len(headers) if headers else (len(rows[0]) if rows else 1)
                nrows = (1 if headers else 0) + len(rows)
                table = doc.add_table(rows=nrows, cols=ncols)
                table.style = "Table Grid"
                if headers:
                    for i, h in enumerate(headers):
                        table.rows[0].cells[i].text = str(h)
                for ri, row in enumerate(rows, start=1 if headers else 0):
                    for ci, val in enumerate(row):
                        table.rows[ri].cells[ci].text = str(val)

        doc.save(path)
        return {"path": path, "title": title, "sections": len(sections or [])}

    def extract_content(self, path: str) -> dict:
        """Extract all text content and structure from a document."""
        doc = Document(path)
        content = []
        for para in doc.paragraphs:
            content.append({
                "text": para.text,
                "style": para.style.name if para.style else None,
                "bold": any(r.bold for r in para.runs),
            })
        tables = []
        for table in doc.tables:
            data = []
            for row in table.rows:
                data.append([cell.text for cell in row.cells])
            tables.append(data)
        return {"path": path, "paragraphs": content, "tables": tables}

    def analyze_document(self, path: str) -> dict:
        """Analyze document structure and statistics."""
        doc = Document(path)
        word_count = 0
        char_count = 0
        for para in doc.paragraphs:
            word_count += len(para.text.split())
            char_count += len(para.text)
        sections_info = []
        for section in doc.sections:
            sections_info.append({
                "page_width": section.page_width,
                "page_height": section.page_height,
                "orientation": "landscape" if section.orientation == WD_ORIENT.LANDSCAPE else "portrait",
            })
        return {
            "path": path,
            "paragraphs": len(doc.paragraphs),
            "tables": len(doc.tables),
            "sections": len(doc.sections),
            "word_count": word_count,
            "char_count": char_count,
            "sections_info": sections_info,
        }

    @staticmethod
    def _parse_color(hex_color: str) -> tuple:
        hex_color = hex_color.lstrip("#")
        if len(hex_color) == 6:
            return tuple(int(hex_color[i:i+2], 16) for i in (0, 2, 4))
        return (0, 0, 0)
