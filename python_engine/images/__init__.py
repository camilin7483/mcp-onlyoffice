"""Image Engine — reusable image capabilities.

Capabilities:
    - Image conversion (PNG, JPEG, SVG, WebP)
    - Image manipulation (resize, crop, rotate)
    - Screenshot generation
    - Document/image integration
"""

import os
from PIL import Image, ImageDraw, ImageFont, ImageFilter


class ImageEngine:
    """Reusable image capabilities. NOT a tool — a library."""

    def convert(self, path: str, target_format: str = "png",
                output_path: str = None) -> dict:
        """Convert an image to a different format."""
        img = Image.open(path)
        fmt = target_format.upper()
        if fmt == "JPG":
            fmt = "JPEG"
        out = output_path or os.path.splitext(path)[0] + f".{target_format.lower()}"
        img.save(out, format=fmt)
        return {"path": out, "format": target_format, "size": os.path.getsize(out)}

    def resize(self, path: str, width: int = None, height: int = None,
               maintain_aspect: bool = True, output_path: str = None) -> dict:
        """Resize an image."""
        img = Image.open(path)
        if maintain_aspect:
            ratio = max(width / img.width if width else 1,
                        height / img.height if height else 1)
            new_size = (int(img.width * ratio), int(img.height * ratio))
        else:
            new_size = (width or img.width, height or img.height)
        resized = img.resize(new_size, Image.LANCZOS)
        out = output_path or path
        resized.save(out)
        return {"path": out, "width": new_size[0], "height": new_size[1],
                "size": os.path.getsize(out)}

    def crop(self, path: str, left: int, top: int,
             right: int, bottom: int, output_path: str = None) -> dict:
        """Crop an image to a specific region."""
        img = Image.open(path)
        cropped = img.crop((left, top, right, bottom))
        out = output_path or path
        cropped.save(out)
        return {"path": out, "width": cropped.width, "height": cropped.height}

    def rotate(self, path: str, degrees: float = 90,
               output_path: str = None) -> dict:
        """Rotate an image."""
        img = Image.open(path)
        rotated = img.rotate(degrees, expand=True)
        out = output_path or path
        rotated.save(out)
        return {"path": out, "degrees": degrees}

    def info(self, path: str) -> dict:
        """Get image metadata."""
        img = Image.open(path)
        return {
            "path": path,
            "format": img.format,
            "mode": img.mode,
            "width": img.width,
            "height": img.height,
            "size": os.path.getsize(path),
        }
