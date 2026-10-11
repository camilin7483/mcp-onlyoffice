"""PDF Engine — reusable PDF capabilities.

Capabilities:
    - Create PDFs from text (cover, brand, pagination)
    - Merge PDFs
    - Extract text from PDFs
    - Convert documents to PDF
"""

import os
from datetime import date


BRAND = "mcp-onlyoffice"
ACCENT = (30, 136, 229)  # blue, matches Q3 deck theme


class PDFEngine:
    """Reusable PDF capabilities. NOT a tool — a library."""

    def create_pdf(self, path: str, text: str = "",
                   title: str = "Document", author: str = None,
                   brand: str = BRAND) -> dict:
        """Create a PDF with cover page, brand footer and page numbers.

        Keeps multi_cell(new_x/new_y) flow for body text. Sets real
        metadata (title/author) so info() reports actual values.
        """
        from fpdf import FPDF

        author = author or brand

        class _ProPDF(FPDF):
            def footer(self):
                if self.page_no() == 1:
                    return  # cover stays clean
                self.set_y(-15)
                self.set_font("Helvetica", "", 8)
                self.set_text_color(120, 120, 120)
                self.cell(0, 10, f"{self._brand}  |  Page {self.page_no()}/{{nb}}",
                          new_x="LMARGIN", new_y="NEXT", align="C")

        pdf = _ProPDF()
        pdf._brand = brand
        pdf.alias_nb_pages("{nb}")
        pdf.set_title(title)
        pdf.set_author(author)

        # Cover page
        pdf.add_page()
        pdf.set_fill_color(*ACCENT)
        pdf.rect(0, 0, 210, 70, style="F")
        pdf.set_y(20)
        pdf.set_font("Helvetica", "B", 28)
        pdf.set_text_color(255, 255, 255)
        pdf.cell(0, 14, title, new_x="LMARGIN", new_y="NEXT", align="C")
        pdf.set_font("Helvetica", "", 12)
        pdf.cell(0, 8, f"{brand}  |  {date.today().isoformat()}",
                 new_x="LMARGIN", new_y="NEXT", align="C")
        pdf.set_y(85)
        pdf.set_text_color(30, 30, 30)

        # Body
        pdf.set_font("Helvetica", "", 11)
        for line in text.split("\n"):
            if line.strip():
                pdf.multi_cell(0, 5, line, new_x="LMARGIN", new_y="NEXT")
        if not text.strip():
            pdf.multi_cell(0, 5, "(empty document)", new_x="LMARGIN", new_y="NEXT")
        pdf.output(path)
        return {"path": path, "size": os.path.getsize(path),
                "pages": pdf.pages_count, "title": title, "author": author}

    def extract_text(self, path: str) -> dict:
        """Extract text content from a PDF."""
        import PyPDF2
        with open(path, "rb") as f:
            reader = PyPDF2.PdfReader(f)
            pages = []
            for page_num in range(len(reader.pages)):
                page = reader.pages[page_num]
                pages.append({
                    "page": page_num + 1,
                    "text": page.extract_text(),
                })
        return {"path": path, "pages": pages, "total_pages": len(pages)}

    def merge_pdfs(self, paths: list, output_path: str) -> dict:
        """Merge multiple PDFs into one."""
        import PyPDF2
        merger = PyPDF2.PdfMerger()
        for p in paths:
            merger.append(p)
        merger.write(output_path)
        merger.close()
        return {"path": output_path, "source_count": len(paths),
                "size": os.path.getsize(output_path)}

    def split_pdf(self, path: str, output_dir: str = None,
                  pages: list = None) -> dict:
        """Split a PDF into single-page (or selected) files.

        Args:
            path: source PDF.
            output_dir: destination dir (defaults to the source dir).
            pages: 1-based page numbers to extract (default: all).
        """
        import PyPDF2
        out_dir = output_dir or os.path.dirname(path) or "."
        os.makedirs(out_dir, exist_ok=True)
        base = os.path.splitext(os.path.basename(path))[0]
        files = []
        with open(path, "rb") as f:
            reader = PyPDF2.PdfReader(f)
            total = len(reader.pages)
            wanted = pages or list(range(1, total + 1))
            for num in wanted:
                if num < 1 or num > total:
                    continue
                writer = PyPDF2.PdfWriter()
                writer.add_page(reader.pages[num - 1])
                out = os.path.join(out_dir, f"{base}_p{num}.pdf")
                with open(out, "wb") as of:
                    writer.write(of)
                files.append(out)
        return {"path": path, "total_pages": total, "files": files,
                "count": len(files)}

    def info(self, path: str) -> dict:
        """Return PDF metadata: page count, size, title, author."""
        import PyPDF2
        with open(path, "rb") as f:
            reader = PyPDF2.PdfReader(f)
            meta = reader.metadata or {}
            info = {"path": path, "pages": len(reader.pages),
                    "size": os.path.getsize(path),
                    "title": getattr(meta, "title", None),
                    "author": getattr(meta, "author", None)}
        return info
