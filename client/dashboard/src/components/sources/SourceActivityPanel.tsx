import { type SourceTelemetrySummary } from "@/components/sources/sourceTelemetrySummary";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
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
        <Text muted small>
          {windowLabel}
        </Text>
      </div>

      {isLoading ? (
        <div className="bg-muted/20 h-48 animate-pulse border p-6" />
      ) : tools.length > 0 ? (
        <div className="space-y-4">
          {summary && <TelemetrySummaryRow summary={summary} />}
          <div className="border p-4">
            <Text muted small className="mb-3 block">
              Tool usage
            </Text>
            <ToolBarList tools={tools} />
          </div>
        </div>
      ) : (
        <div className="flex flex-col items-center justify-center border p-12 text-center">
          <Text muted className="mb-1 block">
            No invocation data yet
          </Text>
          <Text muted small>
            Telemetry will appear here once tools from this source are called
            via an MCP server.
          </Text>
        </div>
      )}
    </div>
  );
}

function TelemetrySummaryRow({ summary }: { summary: SourceTelemetrySummary }) {
  return (
    <div className="flex items-center gap-4 text-sm">
      <Text muted small>
        {summary.totalCalls.toLocaleString()} calls
      </Text>
      {summary.totalFailures > 0 && (
        <Text small className="text-destructive">
          {summary.totalFailures} failed
        </Text>
      )}
      <Text muted small>
        {summary.avgLatency < 1000
          ? `${summary.avgLatency.toFixed(0)}ms avg`
          : `${(summary.avgLatency / 1000).toFixed(1)}s avg`}
      </Text>
      {summary.errorRate > 0 && (
        <Text
          small
          className={
            summary.errorRate > 5 ? "text-destructive" : "text-warning"
          }
        >
          {summary.errorRate.toFixed(1)}% error rate
        </Text>
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
      {barListData.map((item) => {
        const widthPercent = maxValue > 0 ? (item.value / maxValue) * 100 : 0;

        return (
          <div key={item.name} className="flex items-center gap-2">
            <span className="min-w-[3rem] shrink-0 text-right text-sm font-medium">
              {item.value.toLocaleString()}
            </span>
            {/* Single-metric ranked list: one ink fill on a neutral track. */}
            <div className="bg-muted relative h-7 flex-1">
              <span className="text-foreground absolute inset-y-0 left-2 z-0 flex items-center truncate pr-2 text-sm font-medium">
                {item.name}
              </span>
              <div
                className="bg-foreground absolute inset-y-0 left-0"
                style={{ width: `${Math.max(widthPercent, 5)}%` }}
              />
              <div
                className="absolute inset-y-0 left-0 z-10 overflow-hidden"
                style={{ width: `${Math.max(widthPercent, 5)}%` }}
              >
                <span className="text-background absolute inset-y-0 left-2 flex items-center truncate pr-2 text-sm font-medium whitespace-nowrap">
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
