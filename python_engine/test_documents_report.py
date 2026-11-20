import os
import tempfile
import unittest
import zipfile
from pathlib import Path

from PIL import Image

from documents import DocumentEngine


class ReportImageTest(unittest.TestCase):
    def test_report_embeds_image(self):
        with tempfile.TemporaryDirectory() as tmp:
            img_path = os.path.join(tmp, "chart.png")
            Image.new("RGB", (40, 40), (30, 120, 200)).save(img_path)
            out = os.path.join(tmp, "report.docx")
            result = DocumentEngine().create_report(
                out,
                title="Report with image",
                sections=[
                    {"type": "text", "heading": "Intro", "content": "hello"},
                    {"type": "image", "heading": "Chart", "path": img_path, "width_inches": 3},
                ],
            )
            self.assertEqual(result["sections"], 2)
            self.assertTrue(Path(out).is_file())
            with zipfile.ZipFile(out) as zf:
                media = [n for n in zf.namelist() if n.startswith("word/media/")]
                self.assertTrue(media, "no image part embedded")
                xml = zf.read("word/document.xml").decode("utf-8")
                self.assertIn("graphic", xml)
                self.assertIn("Chart", xml)

    def test_missing_image_is_an_error(self):
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(FileNotFoundError):
                DocumentEngine().create_report(
                    os.path.join(tmp, "bad.docx"),
                    sections=[{"type": "image", "path": os.path.join(tmp, "missing.png")}],
                )


if __name__ == "__main__":
    unittest.main()


def test_mail_merge_checks_all_destinations_before_writing(tmp_path):
    from docx import Document
    import pytest

    template = tmp_path / "template.docx"
    doc = Document()
    doc.add_paragraph("Hello {{name}}")
    doc.save(template)
    out_dir = tmp_path / "merged"
    out_dir.mkdir()
    existing = out_dir / "doc_2.docx"
    existing.write_bytes(b"existing document")
    rows = [{"name": "Alice"}, {"name": "Bob"}]
    engine = DocumentEngine()

    with pytest.raises(ValueError, match="already exists"):
        engine.mail_merge(str(template), str(out_dir), rows=rows)

    assert not (out_dir / "doc_1.docx").exists()
    assert existing.read_bytes() == b"existing document"
    result = engine.mail_merge(str(template), str(out_dir), rows=rows[:1])
    assert Document(result["files"][0]).paragraphs[0].text == "Hello Alice"
