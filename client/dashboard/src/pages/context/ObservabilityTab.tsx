import { Badge } from "@/components/ui/badge";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import {
  useObservabilityOverview,
  useSearchLogs,
} from "@/hooks/useObservability";
import type { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord";
import { useMemo, useState } from "react";

// ── ObservabilityTab ─────────────────────────────────────────────────────

export function ObservabilityTab() {
  const [subTab, setSubTab] = useState<"search-logs" | "skill-invocations">(
    "search-logs",
  );

  const now = useMemo(() => new Date(), []);
  const weekAgo = useMemo(
    () => new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000),
    [now],
  );

  const searchLogs = useSearchLogs();
  const { overview } = useObservabilityOverview({ from: weekAgo, to: now });

  const summary = overview?.summary;

  return (
    <div className="space-y-4">
      {/* Summary stats */}
      <div className="grid grid-cols-4 gap-4">
        <StatCard
          label="Total Chats"
          value={String(summary?.totalChats ?? 0)}
        />
        <StatCard
          label="Avg Latency"
          value={`${summary?.avgLatencyMs ?? 0}ms`}
        />
        <StatCard
          label="Total Tool Calls"
          value={String(summary?.totalToolCalls ?? 0)}
        />
        <StatCard
          label="Failed Tool Calls"
          value={String(summary?.failedToolCalls ?? 0)}
        />
      </div>

      {/* Sub-tab toggle */}
      <div className="flex items-center gap-1">
        <LayerToggle
          active={subTab === "search-logs"}
          onClick={() => setSubTab("search-logs")}
        >
          Search Logs
        </LayerToggle>
        <LayerToggle
          active={subTab === "skill-invocations"}
          onClick={() => setSubTab("skill-invocations")}
        >
          Skill Invocations
        </LayerToggle>
      </div>

      {subTab === "search-logs" ? (
        <SearchLogsTable logs={searchLogs.logs} />
      ) : (
        <div className="text-muted-foreground text-sm">
          Skill invocations coming soon.
        </div>
      )}
    </div>
  );
}

// ── StatCard ──────────────────────────────────────────────────────────────

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-card px-4 py-3">
      <Type small muted className="block">
        {label}
      </Type>
      <Type variant="subheading" className="mt-1 block">
        {value}
      </Type>
    </div>
  );
}

// ── LatencyBadge ──────────────────────────────────────────────────────────

function LatencyBadge({ ms }: { ms: number }) {
  const variant = ms < 40 ? "default" : ms < 60 ? "secondary" : "destructive";
  return <Badge variant={variant}>{ms}ms</Badge>;
}

// ── SearchLogsTable ───────────────────────────────────────────────────────

function SearchLogsTable({ logs }: { logs: TelemetryLogRecord[] }) {
  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border">
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Time
              </th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Body
              </th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Severity
              </th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Service
              </th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">
                Trace
              </th>
            </tr>
          </thead>
          <tbody>
            {logs.map((log) => (
              <tr
                key={log.id}
                className="border-b border-border last:border-b-0 hover:bg-muted/50 transition-colors"
              >
                <td className="px-4 py-2.5 text-muted-foreground whitespace-nowrap">
                  {formatNanoTimestamp(log.timeUnixNano)}
                </td>
                <td className="px-4 py-2.5 font-medium max-w-[300px] truncate">
                  {log.body}
                </td>
                <td className="px-4 py-2.5">
                  {log.severityText ? (
                    <Badge variant="secondary">{log.severityText}</Badge>
                  ) : (
                    <span className="text-muted-foreground">&mdash;</span>
                  )}
                </td>
                <td className="px-4 py-2.5">{log.service.name}</td>
                <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                  {log.traceId ? `${log.traceId.slice(0, 12)}...` : "\u2014"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// ── LayerToggle ───────────────────────────────────────────────────────────

function LayerToggle({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "px-2 py-0.5 text-xs rounded-md transition-colors",
        active
          ? "bg-foreground text-background"
          : "text-muted-foreground hover:text-foreground hover:bg-muted/50",
      )}
    >
      {children}
    </button>
  );
}

// ── Helpers ───────────────────────────────────────────────────────────────

function formatNanoTimestamp(nanos: string): string {
  try {
    const ms = Number(BigInt(nanos) / BigInt(1_000_000));
    return new Date(ms).toLocaleTimeString();
  } catch {
    return nanos;
  }
}
