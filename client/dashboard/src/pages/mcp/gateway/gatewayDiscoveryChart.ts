import { formatChartLabel, unixNanoToMs } from "@/components/chart/chartUtils";
import type { GetMetaMcpServerUsageResult } from "@gram/client/models/components/getmetamcpserverusageresult.js";

export type DiscoveryFailures = NonNullable<
  GetMetaMcpServerUsageResult["discoveryFailures"]
>;

export function discoveryChartData(
  failures: DiscoveryFailures | undefined,
  from: Date,
  to: Date,
): {
  bucketMs: number;
  timestamps: number[];
  counts: number[];
  labels: string[];
  tooltipLabels: string[];
} {
  const bucketMs = (failures?.intervalSeconds ?? 3600) * 1000;
  const timestamps =
    failures?.buckets.map((b) => unixNanoToMs(b.bucketTimeUnixNano)) ?? [];
  return {
    bucketMs,
    timestamps,
    counts: failures?.buckets.map((b) => b.count) ?? [],
    labels: timestamps.map((t) =>
      formatChartLabel(new Date(t), to.getTime() - from.getTime()),
    ),
    tooltipLabels: timestamps.map((t) =>
      new Date(t).toLocaleString([], {
        month: "short",
        day: "numeric",
        hour: "numeric",
        minute: "2-digit",
      }),
    ),
  };
}

export function diagnosticMessage(attributes: unknown): string {
  if (attributes && typeof attributes === "object") {
    const attrs = attributes as Record<string, unknown>;
    const error = attrs.error;
    const nested =
      error && typeof error === "object"
        ? (error as Record<string, unknown>).message
        : undefined;
    const message = attrs["error.message"] ?? nested;
    if (typeof message === "string" && message) return message;
  }
  return "A server could not be listed and its tools were omitted.";
}

// Canvas tooltips need explicit wrapping, including long unbroken member names.
export function tooltipLines(message: string): string[] {
  return (
    message.match(/.{1,36}(?:\s|$)|.{1,36}/g)?.map((line) => line.trim()) ?? []
  );
}
