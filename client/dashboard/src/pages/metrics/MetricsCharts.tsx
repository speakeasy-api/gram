import { Metrics } from "@gram/client/models/components";
import { ToolFailures } from "./charts/ToolFailures";
import { ToolCallsByType } from "./charts/ToolCallsByType";

interface MetricsChartsProps {
  metrics: Metrics;
}

function ChartCard({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col rounded-xl border border-border bg-card overflow-hidden">
      <div className="px-4 py-3 border-b border-border bg-muted/30">
        <h3 className="text-sm font-medium">{title}</h3>
      </div>
      <div className="p-4 flex-1">{children}</div>
    </div>
  );
}

export function MetricsCharts({ metrics }: MetricsChartsProps) {
  return (
    <div className="flex flex-col gap-4">
      {/* Most Popular Tools - full width */}
      <ChartCard title="Most Popular Tools">
        <ToolCallsByType tools={metrics.tools} />
      </ChartCard>

      {/* Tools with Most Failures */}
      <ChartCard title="Tools with Most Failures">
        <ToolFailures tools={metrics.tools} />
      </ChartCard>
    </div>
  );
}
