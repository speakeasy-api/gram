"use client";

import { useDensity } from "@/elements/hooks/useDensity";
import { cn } from "@/lib/utils";
import {
  isJsonRenderTree,
  type JsonRenderNode,
} from "@/elements/lib/generative-ui";
import { SyntaxHighlighterProps } from "@assistant-ui/react-markdown";
import { AlertCircleIcon } from "lucide-react";
import { ElementType, FC, useMemo } from "react";
import { MacOSWindowFrame } from "../components/MacOSWindowFrame";
import { PluginLoadingState } from "../components/PluginLoadingState";

// Import all chart components
import {
  BarChart,
  LineChart,
  AreaChart,
  PieChart,
  DonutChart,
  ScatterChart,
  RadarChart,
} from "./ui";

const loadingMessages = [
  "Rendering chart...",
  "Visualizing data...",
  "Building chart...",
  "Processing data...",
];

function getRandomLoadingMessage(): string {
  return loadingMessages[Math.floor(Math.random() * loadingMessages.length)]!;
}

/**
 * Chart components registry. Each entry accepts the chart-specific prop shape
 * declared in `./ui`, but the registry erases those generics via `ElementType`
 * so heterogeneous components can coexist under one key-indexed map.
 */
const chartComponents: Record<string, ElementType> = {
  BarChart,
  LineChart,
  AreaChart,
  PieChart,
  DonutChart,
  ScatterChart,
  RadarChart,
};

/**
 * Render a chart node from json-render tree
 */
function renderChartNode(node: JsonRenderNode): React.ReactNode {
  // Registry entries are heterogeneous (`ElementType` erases each component's
  // prop shape), so re-widen to an untyped-props component for the dynamic
  // JSON-driven render below.
  const Component = chartComponents[node.type] as
    | FC<Record<string, unknown>>
    | undefined;

  if (!Component) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <AlertCircleIcon className="size-4" />
        <span>Unknown chart type: {node.type}</span>
      </div>
    );
  }

  return <Component {...(node.props ?? {})} />;
}

export const ChartRenderer: FC<SyntaxHighlighterProps> = ({ code }) => {
  const d = useDensity();

  // Parse JSON - returns null if invalid (still streaming)
  const content = useMemo(() => {
    const trimmedCode = code.trim();
    if (!trimmedCode) return null;

    try {
      const parsed = JSON.parse(trimmedCode);
      // Validate it has a type field (basic json-render structure)
      if (!isJsonRenderTree(parsed)) {
        return null;
      }
      return parsed;
    } catch {
      // JSON is incomplete (still streaming) - return null to show loading state
      return null;
    }
  }, [code]);

  // Memoize the loading message so it doesn't change on every render
  const loadingMessage = useMemo(() => getRandomLoadingMessage(), []);

  // Show loading shimmer while JSON is incomplete/streaming
  if (!content) {
    return <PluginLoadingState text={loadingMessage} />;
  }

  // Render with macOS-style window frame
  return (
    <MacOSWindowFrame>
      <div className={cn("w-full bg-card", d("p-lg"))}>
        {renderChartNode(content)}
      </div>
    </MacOSWindowFrame>
  );
};
