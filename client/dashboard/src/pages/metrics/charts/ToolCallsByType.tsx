import { useMemo } from "react";
import { ToolUsage } from "@gram/client/models/components";
import { parseToolName, MAX_CHART_ITEMS, CHART_COLORS } from "./utils";
import { HorizontalBarChart } from "./HorizontalBarChart";

interface ToolCallsByTypeProps {
  tools: Array<ToolUsage>;
}

export function ToolCallsByType({ tools }: ToolCallsByTypeProps) {
  const bars = useMemo(() => {
    const sorted = [...tools].sort((a, b) => b.count - a.count).slice(0, MAX_CHART_ITEMS);
    if (sorted.length === 0) return [];
    const max = sorted[0].count;
    return sorted.map((tool) => ({
      label: parseToolName(tool.urn),
      value: tool.count,
      pct: max > 0 ? (tool.count / max) * 100 : 0,
      colorClass: CHART_COLORS.secondary,
    }));
  }, [tools]);

  if (bars.length === 0) {
    return (
      <div className="flex items-center justify-center h-[160px] text-muted-foreground">
        <span className="text-sm">No tool calls recorded</span>
      </div>
    );
  }

  return <HorizontalBarChart bars={bars} />;
}
