/**
 * Apply a centered moving average to smooth data (like DataDog).
 * Adapts window size based on data length for consistent visual smoothing.
 */
export function smoothData(data: number[], windowSize?: number): number[] {
  if (data.length < 3) return data;

  // Scale window size with data length for consistent visual smoothing
  // Fewer points = larger window percentage to maintain smoothness
  let window: number;
  if (windowSize !== undefined) {
    window = windowSize;
  } else if (data.length < 20) {
    // Very zoomed in: use 25-30% of data points
    window = Math.max(3, Math.floor(data.length * 0.3));
  } else if (data.length < 50) {
    // Moderate zoom: use ~15% of data points
    window = Math.max(5, Math.floor(data.length * 0.15));
  } else {
    // Full view: use ~8% of data points, max 21
    window = Math.max(5, Math.min(21, Math.floor(data.length * 0.08)));
  }

  const halfWindow = Math.floor(window / 2);

  return data.map((_, i) => {
    const start = Math.max(0, i - halfWindow);
    const end = Math.min(data.length, i + halfWindow + 1);
    const slice = data.slice(start, end);
    return slice.reduce((a, b) => a + b, 0) / slice.length;
  });
}

/**
 * Returns a formatted label for a chart based on the date and time range
 * - ≤24 hours: Time only "14:00"
 * - ≤2 days: Date & Time "Jan 5, 14:00"
 * - >2 days: Date only "Jan 5"
 */
export function formatChartLabel(date: Date, timeRangeMs: number): string {
  const hours = timeRangeMs / (1000 * 60 * 60);
  const days = hours / 24;
  if (hours <= 24) {
    return date.toLocaleTimeString([], {
      hour: "2-digit",
      minute: "2-digit",
    });
  } else if (days <= 2) {
    return date.toLocaleDateString([], {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  } else {
    return date.toLocaleDateString([], { month: "short", day: "numeric" });
  }
}

export type ThresholdConfig = {
  red: number;
  amber: number;
  inverted?: boolean; // Set to true if lower is better (like latency)
};

export function getValueColor(
  value: number,
  thresholds?: ThresholdConfig,
): string {
  if (!thresholds) return "";

  if (thresholds.inverted) {
    // Lower is better (e.g., latency)
    if (value > thresholds.red) return "text-red-500";
    if (value > thresholds.amber) return "text-amber-500";
    return "text-emerald-600";
  } else {
    // Higher is better (e.g., chats, resolution rate)
    if (value < thresholds.red) return "text-red-500";
    if (value < thresholds.amber) return "text-amber-500";
    return "text-emerald-600";
  }
}
