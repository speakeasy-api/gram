import { useAuiState } from "@assistant-ui/react";
import { useMemo, type FC, type PropsWithChildren } from "react";
import { useElements } from "@/elements/hooks/useElements";
import { humanizeToolName } from "@/elements/lib/humanize";
import {
  isPartialToolCallAnnotation,
  isToolCallAnnotation,
  toolCallAnnotationTitle,
  trailingAnnotationLine,
} from "@/elements/lib/toolCallAnnotation";
import { ToolUIGroup } from "@/elements/components/ui/tool-ui";

/**
 * Renders one tool run: a cluster of tool-call parts plus the terse
 * annotation text parts the assistant emits before each batch (see
 * messagePartGrouping.ts). The heading is the latest annotation in the run,
 * so it advances as the investigation progresses; the annotations' prose
 * renders are suppressed in thread.tsx.
 */
export const ToolGroup: FC<PropsWithChildren<{ indices: number[] }>> = ({
  children,
  indices,
}) => {
  const toolCount = useAuiState(({ message }) => {
    let count = 0;
    for (const i of indices) {
      if (message.parts[i]?.type === "tool-call") count++;
    }
    return count;
  });

  const firstToolName = useAuiState(({ message }) => {
    for (const i of indices) {
      const part = message.parts[i];
      if (part?.type === "tool-call") return part.toolName;
    }
    return undefined;
  });

  const anyMessagePartsAreRunning = useAuiState(({ message }) => {
    for (const i of indices) {
      if (message.parts[i]?.status?.type === "running") return true;
    }
    return false;
  });

  // While the message is still streaming and nothing has been emitted after
  // this run, the next annotation may still replace the heading — keep the
  // group in its running state so the swap shimmers in instead of snapping
  // from a "done" state.
  const isTrailingWhileStreaming = useAuiState(({ message }) => {
    if (message.status?.type !== "running") return false;
    const lastIndex = indices[indices.length - 1] ?? -1;
    for (let i = lastIndex + 1; i < message.parts.length; i++) {
      const part = message.parts[i];
      // An empty text part or a lone annotation (the next batch's heading,
      // mid-flight) doesn't end the run.
      if (
        part?.type === "text" &&
        (part.text.trim().length === 0 ||
          isPartialToolCallAnnotation(part.text))
      ) {
        continue;
      }
      return false;
    }
    return true;
  });

  const isRunning = anyMessagePartsAreRunning || isTrailingWhileStreaming;

  // Latest annotation in the run wins — the heading tracks where the
  // investigation currently is. Mixed prose+annotation blocks stay outside
  // the run (only pure annotations join it), so also check the part just
  // before the run for a trailing annotation line.
  const annotation = useAuiState(({ message }) => {
    for (let k = indices.length - 1; k >= 0; k--) {
      const part = message.parts[indices[k]!];
      if (part?.type === "text" && isToolCallAnnotation(part.text)) {
        return toolCallAnnotationTitle(part.text);
      }
    }
    const first = indices[0] ?? 0;
    const prev = first > 0 ? message.parts[first - 1] : undefined;
    if (prev?.type === "text") {
      const line = trailingAnnotationLine(prev.text);
      if (line) return toolCallAnnotationTitle(line);
    }
    return undefined;
  });

  const { config } = useElements();
  const defaultExpanded = config.tools?.expandToolGroupsByDefault ?? false;

  const groupTitle = useMemo(() => {
    if (annotation) return annotation;
    if (toolCount === 1 && firstToolName) {
      const name = humanizeToolName(firstToolName);
      return anyMessagePartsAreRunning ? `Calling ${name}...` : `Ran ${name}`;
    }
    return anyMessagePartsAreRunning
      ? `Running ${toolCount} tools...`
      : `Ran ${toolCount} tools`;
  }, [annotation, toolCount, firstToolName, anyMessagePartsAreRunning]);

  const status = isRunning ? ("running" as const) : ("complete" as const);

  // If there's a custom component for the single tool, render children
  // directly — but keep the annotation visible, since AssistantText has
  // suppressed its prose render.
  if (
    toolCount === 1 &&
    firstToolName &&
    config.tools?.components?.[firstToolName]
  ) {
    if (!annotation) return children;
    return (
      <>
        <div className="my-1 text-sm text-muted-foreground">{annotation}</div>
        {children}
      </>
    );
  }

  // Single and multiple tool calls must share one wrapper element type —
  // diverging branches would remount every card when a streaming turn grows
  // the group.
  return (
    <div className="my-4 w-full max-w-xl">
      <ToolUIGroup
        title={groupTitle}
        status={status}
        defaultExpanded={defaultExpanded}
      >
        {children}
      </ToolUIGroup>
    </div>
  );
};
