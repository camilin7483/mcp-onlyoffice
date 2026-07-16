"""Analytics Engine — reusable data analysis capabilities.

Capabilities:
    - Statistics (mean, median, std, percentiles)
    - Aggregations (sum, count, group by)
    - KPIs and metrics
    - Predictions (linear regression, trends)
    - Financial analysis (growth rates, ratios)
    - Temporal analysis (trends, seasonality)
    - Anomaly detection
    - Data transformation
"""

import numpy as np
from collections import Counter, defaultdict
import json
import os


class AnalyticsEngine:
    """Reusable analytics capabilities. NOT a tool — a library."""

    def statistics(self, data: list, name: str = "data") -> dict:
        """Compute descriptive statistics for a numeric dataset."""
        arr = np.array(data, dtype=float)
        return {
            "name": name,
            "count": int(len(arr)),
            "mean": float(np.mean(arr)),
            "median": float(np.median(arr)),
            "std": float(np.std(arr, ddof=1)) if len(arr) > 1 else 0,
            "min": float(np.min(arr)),
            "max": float(np.max(arr)),
            "q1": float(np.percentile(arr, 25)),
            "q3": float(np.percentile(arr, 75)),
            "sum": float(np.sum(arr)),
            "variance": float(np.var(arr, ddof=1)) if len(arr) > 1 else 0,
            "range": float(np.max(arr) - np.min(arr)),
        }

    def aggregate(self, data: list, group_key: str = None,
                  value_key: str = None, operation: str = "sum") -> dict:
        """Aggregate data by a key with a given operation."""
        groups = defaultdict(list)
        for item in data:
            key = item.get(group_key, "unknown") if group_key else "all"
            val = item.get(value_key, 0) if value_key else item
            try:
                groups[key].append(float(val))
            except (TypeError, ValueError):
                groups[key].append(0)

        op_map = {
            "sum": sum,
            "mean": lambda x: sum(x) / len(x) if x else 0,
            "count": len,
            "min": min,
            "max": max,
            "std": lambda x: float(np.std(x, ddof=1)) if len(x) > 1 else 0,
        }
        func = op_map.get(operation, sum)
        results = {k: func(v) for k, v in groups.items()}
        return {"operation": operation, "group_by": group_key, "results": results}

    def kpi(self, name: str, current: float, target: float,
            min_val: float = 0, max_val: float = None) -> dict:
        """Compute a single KPI with status."""
        if max_val is not None:
            progress = (current - min_val) / (max_val - min_val)
        else:
            progress = current / target if target else 0
        progress = max(0, min(1, progress))
        status = "green" if progress >= 0.8 else "amber" if progress >= 0.5 else "red"
        return {
            "name": name,
            "current": current,
            "target": target,
            "progress": round(progress * 100, 1),
            "gap": round(target - current, 2),
            "status": status,
        }

    def linear_regression(self, x_data: list, y_data: list) -> dict:
        """Perform linear regression and return slope, intercept, R²."""
        x = np.array(x_data, dtype=float)
        y = np.array(y_data, dtype=float)
        n = len(x)
        sx = np.sum(x)
        sy = np.sum(y)
        sxy = np.sum(x * y)
        sx2 = np.sum(x ** 2)
        slope = (n * sxy - sx * sy) / (n * sx2 - sx ** 2)
        intercept = (sy - slope * sx) / n
        y_pred = slope * x + intercept
        ss_res = np.sum((y - y_pred) ** 2)
        ss_tot = np.sum((y - np.mean(y)) ** 2)
        r2 = 1 - (ss_res / ss_tot) if ss_tot != 0 else 0
        return {
            "slope": round(float(slope), 4),
            "intercept": round(float(intercept), 4),
            "r_squared": round(float(r2), 4),
            "formula": f"y = {slope:.4f}x + {intercept:.4f}",
        }

    def predict(self, x_data: list, y_data: list, x_predict: list) -> dict:
        """Predict y values for given x values using linear regression."""
        reg = self.linear_regression(x_data, y_data)
        predictions = [reg["slope"] * x + reg["intercept"] for x in x_predict]
        return {
            "model": reg,
            "x_input": x_predict,
            "predictions": [round(p, 4) for p in predictions],
        }

    def growth_rate(self, values: list) -> dict:
        """Compute period-over-period growth rates."""
        rates = []
        for i in range(1, len(values)):
            prev = values[i - 1]
            if prev != 0:
                rate = (values[i] - prev) / prev * 100
            else:
                rate = 0
            rates.append(round(rate, 2))
        return {
            "values": values,
            "growth_rates": rates,
            "cagr": self._cagr(values),
            "total_growth": self._total_growth(values),
        }

    def _cagr(self, values: list) -> float:
        if len(values) < 2 or values[0] == 0:
            return 0
        n = len(values) - 1
        return round((values[-1] / values[0]) ** (1 / n) - 1, 4)

    def _total_growth(self, values: list) -> float:
        if len(values) < 2 or values[0] == 0:
            return 0
        return round((values[-1] - values[0]) / values[0] * 100, 2)

    def detect_anomalies(self, data: list, method: str = "zscore",
                         threshold: float = 2.0) -> dict:
        """Detect anomalies in a dataset using z-score or IQR."""
        arr = np.array(data, dtype=float)
        if method == "zscore":
            mean = np.mean(arr)
            std = np.std(arr, ddof=1)
            if std == 0:
                return {"method": method, "anomalies": [], "count": 0}
            z_scores = np.abs((arr - mean) / std)
            anomalies = [int(i) for i in range(len(arr)) if z_scores[i] > threshold]
            return {
                "method": method,
                "threshold": threshold,
                "anomalies": anomalies,
                "count": len(anomalies),
                "z_scores": [round(float(z), 3) for z in z_scores],
            }
        elif method == "iqr":
            q1 = np.percentile(arr, 25)
            q3 = np.percentile(arr, 75)
            iqr = q3 - q1
            lower = q1 - 1.5 * iqr
            upper = q3 + 1.5 * iqr
            anomalies = [int(i) for i in range(len(arr))
                         if arr[i] < lower or arr[i] > upper]
            return {
                "method": method,
                "q1": float(q1),
                "q3": float(q3),
                "iqr": float(iqr),
                "lower_bound": float(lower),
                "upper_bound": float(upper),
                "anomalies": anomalies,
                "count": len(anomalies),
            }
        return {"method": method, "error": f"Unknown method: {method}"}

    def correlation(self, data: dict) -> dict:
        """Compute pairwise Pearson correlation between multiple series."""
        keys = list(data.keys())
        if len(keys) < 2:
            return {"error": "Need at least 2 series"}
        matrix = {}
        for i, k1 in enumerate(keys):
            matrix[k1] = {}
            for j, k2 in enumerate(keys):
                v1 = np.array(data[k1], dtype=float)
                v2 = np.array(data[k2], dtype=float)
                matrix[k1][k2] = round(float(np.corrcoef(v1, v2)[0, 1]), 4)
        return {"series": keys, "matrix": matrix}

    def frequency(self, data: list) -> dict:
        """Compute frequency distribution of categorical data."""
        counter = Counter(data)
        total = len(data)
        distribution = {}
        for item, count in counter.most_common():
            distribution[item] = {
                "count": count,
                "percentage": round(count / total * 100, 1),
            }
        return {
            "total": total,
            "unique": len(counter),
            "distribution": distribution,
        }

    def transform(self, data: list, operation: str = "normalize",
                  **kwargs) -> dict:
        """Transform a dataset: normalize, standardize, rank, lag."""
        arr = np.array(data, dtype=float)
        result = []
        method = operation.lower()
        if method == "normalize":
            mn, mx = np.min(arr), np.max(arr)
            result = ((arr - mn) / (mx - mn) * 100).tolist()
        elif method == "standardize":
            mean, std = np.mean(arr), np.std(arr, ddof=1)
            result = ((arr - mean) / std).tolist()
        elif method == "rank":
            result = [int(r) for r in (len(arr) - np.argsort(np.argsort(arr)))]
        elif method == "lag":
            shift = kwargs.get("shift", 1)
            result = [None] * shift + arr[:-shift].tolist()
        elif method == "diff":
            result = [None] + [arr[i] - arr[i - 1] for i in range(1, len(arr))]
        elif method == "cumsum":
            result = np.cumsum(arr).tolist()
        else:
            return {"error": f"Unknown transform: {operation}"}
        return {
            "operation": operation,
            "input_length": len(data),
            "result": [round(float(x), 4) if x is not None else None for x in result],
        }
