"""PDF Engine — reusable PDF capabilities.

Capabilities:
    - Create PDFs from text
    - Merge PDFs
    - Extract text from PDFs
    - Convert documents to PDF
"""

import os


class PDFEngine:
    """Reusable PDF capabilities. NOT a tool — a library."""

    def create_pdf(self, path: str, text: str = "",
                   title: str = "Document") -> dict:
        """Create a simple PDF with text content."""
        from fpdf import FPDF
        pdf = FPDF()
        pdf.add_page()
        pdf.set_font("Arial", "B", 16)
        pdf.cell(0, 10, title, new_x="LMARGIN", new_y="NEXT", align="C")
        pdf.ln(5)
        pdf.set_font("Arial", "", 11)
        for line in text.split("\n"):
            if line.strip():
                pdf.multi_cell(0, 5, line)
        pdf.output(path)
        return {"path": path, "size": os.path.getsize(path), "pages": pdf.pages_count}

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
