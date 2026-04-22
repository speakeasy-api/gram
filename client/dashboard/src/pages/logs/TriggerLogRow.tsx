import { TelemetryLogRecord } from "@gram/client/models/components";
import { Zap } from "lucide-react";
import { StatusBadge } from "./StatusBadge";
import { formatNanoTimestamp } from "./utils";
import type { TraceSummary } from "./use-attribute-logs-query";

function getAttr(attrs: Record<string, unknown>, key: string): unknown {
  if (!attrs || typeof attrs !== "object") return undefined;
  const parts = key.split(".");
  let cur: unknown = attrs;
  for (const part of parts) {
    if (cur == null || typeof cur !== "object") return undefined;
    cur = (cur as Record<string, unknown>)[part];
  }
  return cur;
}

interface TriggerLogRowProps {
  trace: TraceSummary;
  onLogClick: (log: TelemetryLogRecord) => void;
}

export function TriggerLogRow({ trace, onLogClick }: TriggerLogRowProps) {
  const log = trace.log;
  const severity = log?.severityText || "INFO";
  const body = log?.body || "trigger event";
  const deliveryStatus =
    log &&
    typeof getAttr(log.attributes, "gram.trigger.delivery_status") === "string"
      ? (getAttr(log.attributes, "gram.trigger.delivery_status") as string)
      : undefined;

  const isSuccess = deliveryStatus === "sent";
  const isSkipped = deliveryStatus === "skipped";

  return (
    <div className="border-border/50 border-b last:border-b-0">
      <div
        className="hover:bg-muted/50 flex cursor-pointer items-center gap-3 px-8 py-2.5 transition-colors"
        onClick={() => log && onLogClick(log)}
      >
        <div className="text-muted-foreground w-[150px] shrink-0 font-mono text-sm whitespace-nowrap">
          {formatNanoTimestamp(trace.startTimeUnixNano)}
        </div>

        <div className="flex w-5 shrink-0 items-center justify-center">
          <Zap className="text-muted-foreground size-4" strokeWidth={1.5} />
        </div>

        <div className="flex min-w-0 flex-1 items-center gap-2">
          <span className="bg-muted text-muted-foreground shrink-0 rounded px-1.5 py-0.5 text-xs font-medium">
            trigger
          </span>
          <span className="truncate text-sm">{body}</span>
        </div>

        {trace.logCount > 1 && (
          <span className="text-muted-foreground shrink-0 text-xs tabular-nums">
            {trace.logCount} events
          </span>
        )}

        <StatusBadge
          isSuccess={isSuccess}
          severity={isSkipped ? "WARN" : isSuccess ? undefined : severity}
        />
      </div>
    </div>
  );
}
