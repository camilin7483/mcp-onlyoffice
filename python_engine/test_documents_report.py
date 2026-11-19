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
