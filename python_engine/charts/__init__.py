"""Chart Engine — reusable chart generation capabilities.

Capabilities:
    - Bar, line, pie, area, scatter charts
    - Histograms, heatmaps
    - Financial charts (candlestick)
    - KPI charts and dashboards
    - Image export (PNG, SVG)
    - Excel and PowerPoint integration
"""

import os
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import matplotlib.ticker as ticker
import numpy as np
import base64
from io import BytesIO


BRAND_COLORS = ["#1F4E79", "#2E75B6", "#ED7D31",
                "#A5A5A5", "#5B9BD5", "#70AD47", "#264478", "#9E480E"]
CHART_STYLE = "seaborn-v0_8"


def _apply_style() -> None:
    """Use the pro chart style with fallback when unavailable."""
    try:
        plt.style.use(CHART_STYLE)
    except OSError:
        plt.style.use("ggplot")


class ChartEngine:
    """Reusable chart capabilities. NOT a tool — a library."""

    def _save(self, path: str = None, format: str = "png", dpi: int = 150) -> dict:
        """Save the current figure and return result."""
        buf = BytesIO()
        plt.savefig(buf, format=format, dpi=dpi, bbox_inches="tight")
        buf.seek(0)
        result = {"buffer": buf, "format": format, "dpi": dpi}

        if path:
            os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
            with open(path, "wb") as f:
                f.write(buf.getvalue())
            result["path"] = path
            result["size"] = os.path.getsize(path)

        plt.close()
        return result

    def bar_chart(self, categories: list, values: list,
                  title: str = "Bar Chart", xlabel: str = None,
                  ylabel: str = None, color: str = "#1F4E79",
                  horizontal: bool = False, path: str = None,
                  format: str = "png", dpi: int = 150,
                  colors: list = None) -> dict:
        """Create a bar chart with value labels, grid and brand palette."""
        _apply_style()
        plt.figure(figsize=(10, 6))
        palette = colors or [color]
        bar_colors = [palette[i % len(palette)] for i in range(len(categories))]
        y_pos = range(len(categories))
        if horizontal:
            bars = plt.barh(y_pos, values, color=bar_colors, edgecolor="white")
            plt.yticks(list(y_pos), categories)
            plt.bar_label(bars, fmt="%.1f", padding=3)
            plt.grid(True, axis="x", alpha=0.3)
        else:
            bars = plt.bar(y_pos, values, color=bar_colors, edgecolor="white")
            plt.xticks(list(y_pos), categories, rotation=45, ha="right")
            plt.bar_label(bars, fmt="%.1f", padding=3)
            plt.grid(True, axis="y", alpha=0.3)
        plt.title(title, fontsize=14, pad=15)
        if xlabel:
            plt.xlabel(xlabel)
        if ylabel:
            plt.ylabel(ylabel)
        plt.tight_layout()
        return self._save(path, format, dpi=dpi)

    def line_chart(self, x_data: list, y_series: list,
                   labels: list = None, title: str = "Line Chart",
                   xlabel: str = None, ylabel: str = None,
                   path: str = None, format: str = "png",
                   dpi: int = 150, colors: list = None) -> dict:
        """Create a line chart with one or more series, grid and labels."""
        _apply_style()
        plt.figure(figsize=(10, 6))
        palette = colors or BRAND_COLORS
        if isinstance(y_series[0], (list, tuple)):
            for i, ys in enumerate(y_series):
                label = labels[i] if labels and i < len(labels) else f"Series {i+1}"
                (line,) = plt.plot(x_data[:len(ys)], ys, marker="o",
                                   color=palette[i % len(palette)],
                                   linewidth=2, label=label)
                for x, y in zip(x_data[:len(ys)], ys):
                    plt.text(x, y, f"{y}", ha="center", va="bottom",
                             fontsize=8, color=line.get_color())
            plt.legend()
        else:
            (line,) = plt.plot(x_data, y_series, marker="o",
                               color=palette[0], linewidth=2)
            for x, y in zip(x_data, y_series):
                plt.text(x, y, f"{y}", ha="center", va="bottom", fontsize=8)
        plt.title(title, fontsize=14, pad=15)
        if xlabel:
            plt.xlabel(xlabel)
        if ylabel:
            plt.ylabel(ylabel)
        plt.grid(True, alpha=0.3)
        plt.tight_layout()
        return self._save(path, format, dpi=dpi)

    def pie_chart(self, labels: list, values: list,
                  title: str = "Pie Chart", path: str = None,
                  format: str = "png", colors: list = None,
                  dpi: int = 150) -> dict:
        """Create a pie chart with pct value labels and brand palette."""
        _apply_style()
        plt.figure(figsize=(8, 8))
        default_colors = BRAND_COLORS
        wedges, texts, autotexts = plt.pie(
            values, labels=labels, autopct="%1.1f%%",
            colors=colors or default_colors, startangle=90,
            textprops={"fontsize": 11},
        )
        plt.title(title, fontsize=14, pad=15)
        plt.axis("equal")
        plt.tight_layout()
        return self._save(path, format, dpi=dpi)

    def histogram(self, data: list, bins: int = 10,
                  title: str = "Histogram", xlabel: str = None,
                  ylabel: str = "Frequency", color: str = "#4472C4",
                  path: str = None, format: str = "png") -> dict:
        """Create a histogram."""
        plt.figure(figsize=(10, 6))
        plt.hist(data, bins=bins, color=color, edgecolor="white", alpha=0.8)
        plt.title(title, fontsize=14, pad=15)
        plt.xlabel(xlabel or "Value")
        plt.ylabel(ylabel)
        plt.grid(True, alpha=0.3)
        plt.tight_layout()
        return self._save(path, format)

    def heatmap(self, data: list, row_labels: list = None,
                col_labels: list = None, title: str = "Heatmap",
                cmap: str = "Blues", path: str = None,
                format: str = "png") -> dict:
        """Create a heatmap from a 2D data array."""
        plt.figure(figsize=(10, 8))
        data_arr = np.array(data)
        im = plt.imshow(data_arr, cmap=cmap, aspect="auto")
        plt.colorbar(im, shrink=0.8)
        if row_labels:
            plt.yticks(range(len(row_labels)), row_labels)
        if col_labels:
            plt.xticks(range(len(col_labels)), col_labels, rotation=45, ha="right")
        for i in range(data_arr.shape[0]):
            for j in range(data_arr.shape[1]):
                text_color = "white" if data_arr[i, j] > data_arr.max() * 0.6 else "black"
                plt.text(j, i, f"{data_arr[i, j]:.1f}",
                         ha="center", va="center", color=text_color, fontsize=9)
        plt.title(title, fontsize=14, pad=15)
        plt.tight_layout()
        return self._save(path, format)

    def financial_chart(self, dates: list, open_prices: list,
                        close_prices: list, high_prices: list = None,
                        low_prices: list = None,
                        title: str = "Financial Chart",
                        path: str = None, format: str = "png") -> dict:
        """Create a financial chart (candlestick-like with price line)."""
        plt.figure(figsize=(12, 7))
        x = range(len(dates))
        plt.plot(x, close_prices, color="#4472C4", linewidth=2, label="Close")
        plt.fill_between(x, open_prices, close_prices,
                         where=np.array(close_prices) >= np.array(open_prices),
                         color="#70AD47", alpha=0.3, label="Gain")
        plt.fill_between(x, open_prices, close_prices,
                         where=np.array(close_prices) < np.array(open_prices),
                         color="#FF0000", alpha=0.3, label="Loss")
        if len(dates) <= 30:
            plt.xticks(x, dates, rotation=45, ha="right", fontsize=9)
        else:
            step = max(1, len(dates) // 15)
            plt.xticks(x[::step], dates[::step], rotation=45, ha="right", fontsize=9)
        plt.title(title, fontsize=14, pad=15)
        plt.ylabel("Price")
        plt.legend()
        plt.grid(True, alpha=0.3)
        plt.tight_layout()
        return self._save(path, format)

    def kpi_gauge(self, value: float, target: float = 100,
                  title: str = "KPI", min_val: float = 0,
                  max_val: float = 100, path: str = None,
                  format: str = "png") -> dict:
        """Create a single KPI gauge chart."""
        plt.figure(figsize=(6, 4))
        progress = min((value - min_val) / (max_val - min_val), 1.0)
        color = "#70AD47" if progress >= 0.8 else "#FFC000" if progress >= 0.5 else "#FF0000"
        plt.barh(0, progress, color=color, height=0.3)
        plt.barh(0, 1, color="#E0E0E0", height=0.3, alpha=0.3)
        plt.xlim(0, 1)
        plt.yticks([])
        plt.title(title, fontsize=14, pad=15)
        plt.text(progress, 0, f"{value:.1f}", ha="center", va="center",
                 fontsize=20, fontweight="bold", color=color)
        plt.tight_layout()
        return self._save(path, format)

    def export_to_base64(self, path: str = None, format: str = "png") -> dict:
        """Export the last chart as base64-encoded string."""
        result = self._save(path, format)
        if result.get("buffer"):
            result["base64"] = base64.b64encode(result["buffer"].getvalue()).decode()
            del result["buffer"]
        return result

    def embed_in_excel(self, chart_path: str, excel_path: str,
                       sheet_name: str = None, cell: str = "H2",
                       width: int = 640, height: int = 480) -> dict:
        """Embed an existing chart image (PNG) into a workbook sheet.

        Compose with bar_chart/line_chart/... via their `path` param:
        generate the PNG first, then embed it here.
        """
        import os as _os
        from openpyxl import load_workbook as _load
        from openpyxl.drawing.image import Image as _XLImage
        if not _os.path.exists(chart_path):
            return {"status": "error", "error": f"Chart image not found: {chart_path}"}
        wb = _load(excel_path)
        ws = wb[sheet_name] if sheet_name else wb.active
        img = _XLImage(chart_path)
        img.width = width
        img.height = height
        img.anchor = cell
        ws.add_image(img)
        wb.save(excel_path)
        wb.close()
        return {"path": excel_path, "chart": chart_path, "cell": cell}
