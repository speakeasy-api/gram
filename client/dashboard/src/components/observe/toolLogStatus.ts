import type { ToolUsageTraceSummary } from "@gram/client/models/components/toolusagetracesummary.js";
import { isStalePendingToolCall } from "@/lib/toolCallStatus";

export type ToolLogStatusConfig = {
  className: string;
  dotClassName: string;
  label: string;
};

// Status renders as a plain mono word: color only where attention is due
// (error red, blocked orange); success, pending and unknown stay muted ink.
export function getToolLogStatusConfig(
  trace: Pick<
    ToolUsageTraceSummary,
    "hookStatus" | "httpStatusCode" | "startTimeUnixNano"
  >,
  nowMs: number = Date.now(),
): ToolLogStatusConfig | null {
  const ok = {
    className: "text-muted-foreground",
    dotClassName: "bg-emerald-500",
    label: "Success",
  };
  const failed = {
    className: "text-destructive",
    dotClassName: "bg-rose-500",
    label: "Error",
  };

  if (trace.hookStatus) {
    switch (trace.hookStatus) {
      case "blocked":
        // Blocked is the same answer to "did this call run" (it did not), but
        // it is the policy working rather than something broken, so it takes
        // amber and leaves red to mean failure.
        return { ...failed, dotClassName: "bg-amber-500", label: "Blocked" };
      case "failure":
        return failed;
      case "success":
        return ok;
      case "pending": {
        // A hook call with no result is pending while it may still be
        // running. Long after it started, the call has ended without
        // reporting a result, so its outcome is unknown.
        const startedAtMs = Number(
          BigInt(trace.startTimeUnixNano) / 1_000_000n,
        );
        return {
          className: "text-muted-foreground",
          dotClassName: "bg-muted-foreground/30",
          label: isStalePendingToolCall(startedAtMs, nowMs)
            ? "Unknown"
            : "Pending",
        };
      }
      default:
        return null;
    }
  }

  if (trace.httpStatusCode !== undefined) {
    if (trace.httpStatusCode >= 400) return failed;
    if (trace.httpStatusCode >= 200 && trace.httpStatusCode < 400) return ok;
  }

  return null;
}
