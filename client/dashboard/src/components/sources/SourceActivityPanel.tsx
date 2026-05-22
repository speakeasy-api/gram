import { type SourceTelemetrySummary } from "@/components/sources/sourceTelemetrySummary";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import type { ToolMetric } from "@gram/client/models/components";

// Brand-inspired muted palette (from moonshine gradient colors). Defined here
// so both Source overview tabs (OpenAPI + Remote MCP) share one palette.
const barColors = [
  "bg-[hsl(214,69%,50%)]",
  "bg-[hsl(4,67%,52%)]",
  "bg-[hsl(108,35%,45%)]",
  "bg-[hsl(216,70%,60%)]",
  "bg-[hsl(23,80%,55%)]",
  "bg-[hsl(334,50%,45%)]",
  "bg-[hsl(68,45%,50%)]",
  "bg-[hsl(154,50%,40%)]",
  "bg-[hsl(220,60%,45%)]",
  "bg-[hsl(280,40%,50%)]",
];

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
}: SourceActivityPanelProps) {
  return (
    <div className="flex flex-col">
      <div className="mb-3 flex items-center justify-between">
        <Heading variant="h4">Source Activity</Heading>
        <Type muted small>
          {windowLabel}
        </Type>
      </div>

      {isLoading ? (
        <div className="bg-muted/20 h-48 animate-pulse rounded-lg border p-6" />
      ) : tools.length > 0 ? (
        <div className="space-y-4">
          {summary && <TelemetrySummaryRow summary={summary} />}
          <div className="rounded-lg border p-4">
            <Type muted small className="mb-3 block">
              Tool usage
            </Type>
            <ToolBarList tools={tools} />
          </div>
        </div>
      ) : (
        <div className="flex flex-col items-center justify-center rounded-lg border p-12 text-center">
          <Type muted className="mb-1 block">
            No invocation data yet
          </Type>
          <Type muted small>
            Telemetry will appear here once tools from this source are called
            via an MCP server.
          </Type>
        </div>
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
  const barListData = tools.slice(0, 10).map((tool) => ({
    name: tool.gramUrn.replace("tools:", ""),
    value: tool.callCount,
  }));

  if (barListData.length === 0) {
    return (
      <div className="text-muted-foreground py-8 text-center">
        No tool data available
      </div>
    );
  }

  const maxValue = Math.max(...barListData.map((d) => d.value));

  return (
    <div className="space-y-2">
      {barListData.map((item, index) => {
        const widthPercent = maxValue > 0 ? (item.value / maxValue) * 100 : 0;

        return (
          <div key={item.name} className="flex items-center gap-2">
            <span className="min-w-[3rem] shrink-0 text-right text-sm font-medium">
              {item.value.toLocaleString()}
            </span>
            <div className="relative h-7 flex-1">
              <span className="text-foreground absolute inset-y-0 left-2 z-0 flex items-center truncate pr-2 text-sm font-medium">
                {item.name}
              </span>
              <div
                className={`absolute inset-y-0 left-0 rounded ${barColors[index % barColors.length]}`}
                style={{ width: `${Math.max(widthPercent, 5)}%` }}
              />
              <div
                className="absolute inset-y-0 left-0 z-10 overflow-hidden"
                style={{ width: `${Math.max(widthPercent, 5)}%` }}
              >
                <span className="absolute inset-y-0 left-2 flex items-center truncate pr-2 text-sm font-medium whitespace-nowrap text-white">
                  {item.name}
                </span>
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}
