import { useMemo } from "react";
import { ToolUsage } from "@gram/client/models/components";
import { parseToolName, MAX_CHART_ITEMS, CHART_COLORS } from "./utils";
import { HorizontalBarChart } from "./HorizontalBarChart";

interface ToolFailuresProps {
  tools: Array<ToolUsage>;
}

export function ToolFailures({ tools }: ToolFailuresProps) {
  const bars = useMemo(() => {
    const sorted = [...tools]
      .filter((t) => t.failureCount > 0)
      .sort((a, b) => b.failureCount - a.failureCount)
      .slice(0, MAX_CHART_ITEMS);
    if (sorted.length === 0) return [];
    const max = sorted[0].failureCount;
    return sorted.map((tool) => ({
      label: parseToolName(tool.urn),
      value: tool.failureCount,
      pct: max > 0 ? (tool.failureCount / max) * 100 : 0,
      colorClass: CHART_COLORS.destructive,
    }));
  }, [tools]);

  if (bars.length === 0) {
    return (
      <div className="flex items-center justify-center h-[160px] text-muted-foreground">
        <span className="text-sm">No tool failures recorded</span>
      </div>
    );
  }

  return <HorizontalBarChart bars={bars} />;
}
