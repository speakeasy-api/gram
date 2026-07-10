import { type SourceTelemetrySummary } from "@/components/sources/sourceTelemetrySummary";
import { Heading } from "@/components/ui/heading";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { RankedBar } from "@/components/chart/RankedBar";
import { Activity } from "lucide-react";
import type { ToolMetric } from "@gram/client/models/components/toolmetric.js";

export interface SourceActivityPanelProps {
  tools: ToolMetric[];
  summary: SourceTelemetrySummary | null;
  isLoading: boolean;
  // Window label rendered to the right of the heading (e.g. "Last 7 days").
  windowLabel: string;
}

export function SourceActivityPanel({
  tools,
  summary,
  isLoading,
  windowLabel,
}: SourceActivityPanelProps): JSX.Element {
  return (
    <div className="flex flex-col">
      <div className="mb-3 flex items-center justify-between">
        <Heading variant="h4">Source Activity</Heading>
        <Type muted small>
          {windowLabel}
        </Type>
      </div>

      {isLoading ? (
        <div className="border p-6">
          <Skeleton className="mb-4 h-4 w-32" />
          <div className="space-y-3">
            <Skeleton className="h-6 w-full" />
            <Skeleton className="h-6 w-full" />
            <Skeleton className="h-6 w-3/4" />
          </div>
        </div>
      ) : tools.length > 0 ? (
        <div className="space-y-4">
          {summary && <TelemetrySummaryRow summary={summary} />}
          <div className="border p-4">
            <Type muted small className="mb-3 block">
              Tool usage
            </Type>
            <ToolBarList tools={tools} />
          </div>
        </div>
      ) : (
        <InlineEmptyState
          icon={<Activity />}
          title="No invocation data yet"
          description="Telemetry will appear here once tools from this source are called via an MCP server."
        />
      )}
    </div>
  );
}

function TelemetrySummaryRow({ summary }: { summary: SourceTelemetrySummary }) {
  return (
    <div className="flex items-center gap-4 text-sm">
      <Type muted small>
        {summary.totalCalls.toLocaleString()} calls
      </Type>
      {summary.totalFailures > 0 && (
        <Type small className="text-destructive">
          {summary.totalFailures} failed
        </Type>
      )}
      <Type muted small>
        {summary.avgLatency < 1000
          ? `${summary.avgLatency.toFixed(0)}ms avg`
          : `${(summary.avgLatency / 1000).toFixed(1)}s avg`}
      </Type>
      {summary.errorRate > 0 && (
        <Type
          small
          className={
            summary.errorRate > 5 ? "text-destructive" : "text-warning"
          }
        >
          {summary.errorRate.toFixed(1)}% error rate
        </Type>
      )}
    </div>
  );
}

function ToolBarList({ tools }: { tools: ToolMetric[] }) {
  const items = tools.slice(0, 10).map((tool) => ({
    label: tool.gramUrn.replace("tools:", ""),
    value: tool.callCount,
  }));

  if (items.length === 0) {
    return (
      <Type muted small className="block py-8 text-center">
        No tool data available
      </Type>
    );
  }

  return <RankedBar items={items} colorMode="rank-gradient" />;
}
