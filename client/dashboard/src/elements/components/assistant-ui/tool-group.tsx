import { useAuiState } from "@assistant-ui/react";
import { useMemo, type FC, type PropsWithChildren } from "react";
import { useElements } from "@/elements/hooks/useElements";
import { useToolActivitySummary } from "@/elements/hooks/useToolActivitySummary";
import { ToolUIGroup } from "@/elements/components/ui/tool-ui";
import type { ToolActivityCall } from "@/elements/types";

/**
 * latestUserText returns the text of the most recent user message in the
 * thread — the prompt that initiated the current turn — used to ground the
 * tool-activity summary in the user's intent. On the assistant-ui runtime
 * message the text lives under `parts` (matching every other consumer, e.g.
 * useFollowOnSuggestions), not `content`.
 */
function latestUserText(messages: readonly unknown[]): string | undefined {
  for (let i = messages.length - 1; i >= 0; i--) {
    const message = messages[i] as
      | { role?: string; parts?: Array<{ type?: string; text?: string }> }
      | undefined;
    if (message?.role !== "user") continue;
    const text = (message.parts ?? [])
      .filter((part) => part?.type === "text" && typeof part.text === "string")
      .map((part) => part.text)
      .join("")
      .trim();
    return text.length > 0 ? text : undefined;
  }
  return undefined;
}

export const ToolGroup: FC<
  PropsWithChildren<{ startIndex: number; endIndex: number }>
> = ({ children, startIndex, endIndex }) => {
  // startIndex/endIndex are inclusive indices into message.parts.
  // assistant-ui only groups consecutive tool-call parts, so every part
  // in the range is a tool-call.
  const { config } = useElements();
  const defaultExpanded = config.tools?.expandToolGroupsByDefault ?? false;

  const firstToolName = useAuiState(({ message }) => {
    const part = message.parts[startIndex];
    return part?.type === "tool-call" ? part.toolName : undefined;
  });
  const anyMessagePartsAreRunning = useAuiState(({ message }) => {
    for (let i = startIndex; i <= endIndex; i++) {
      if (message.parts[i]?.status?.type === "running") return true;
    }
    return false;
  });
  // Whether this group belongs to the turn currently in flight. Part status is
  // not a reliable liveness signal here: tools executed server-side (the Project
  // Assistant) stream in already-complete, so a group can belong to a live turn
  // without any part ever being observed `running`. The thread's own state is,
  // and it is what decides whether summarizing is worth a model call.
  const threadIsRunning = useAuiState(({ thread }) => thread.isRunning);

  // Serialize the group's tool calls to a stable string so useAuiState only
  // triggers a re-render when they actually change, then parse once.
  //
  // Arguments are read only from calls that have finished streaming: mid-stream
  // `argsText` grows by a token at a time, and including it would re-run this
  // selector (and the stringify/parse round trip) on every one of them for a
  // value the summary can't use until it's complete anyway.
  const toolCallsJson = useAuiState(({ message }) => {
    const calls: ToolActivityCall[] = [];
    for (let i = startIndex; i <= endIndex; i++) {
      const part = message.parts[i];
      if (part?.type !== "tool-call") continue;
      let args: string | undefined;
      if (part.status?.type !== "running") {
        if (typeof part.argsText === "string" && part.argsText.length > 0) {
          args = part.argsText;
        } else if (part.args != null) {
          try {
            args = JSON.stringify(part.args);
          } catch {
            args = undefined;
          }
        }
      }
      calls.push({ name: part.toolName, arguments: args });
    }
    return JSON.stringify(calls);
  });
  const toolCalls = useMemo<ToolActivityCall[]>(
    () => JSON.parse(toolCallsJson) as ToolActivityCall[],
    [toolCallsJson],
  );

  // The prompt that initiated this turn. Read from thread state rather than
  // memoized on the runtime identity: the runtime object is stable for the
  // thread's whole life, so a memo keyed on it never re-runs and a group that
  // mounted before the user message was in state would keep an empty prompt —
  // leaving the summary with nothing to ground itself in.
  const userMessage = useAuiState(({ thread }) =>
    latestUserText(thread.messages),
  );

  // A single tool with a custom component renders directly and never shows this
  // label, so don't spend a summarization call on it.
  const hasCustomComponent =
    toolCalls.length === 1 &&
    !!firstToolName &&
    !!config.tools?.components?.[firstToolName];

  const { label, pending } = useToolActivitySummary({
    toolCalls,
    inProgress: anyMessagePartsAreRunning,
    isLiveTurn: threadIsRunning,
    userMessage,
    enabled: !hasCustomComponent,
  });

  // If there's a custom component for the single tool, render children directly.
  if (hasCustomComponent) {
    return children;
  }

  const running = anyMessagePartsAreRunning || pending;

  // Present tool activity as a single human-readable "task" line with the
  // individual calls collapsed behind it — users rarely need the raw
  // inputs/outputs, so lead with what the agent is doing, not how.
  return (
    <div className="my-4 w-full max-w-xl">
      <ToolUIGroup
        title={label}
        // Spinner only while the tools themselves run; once they finish the
        // thread's indicator takes the wait over, so a second spinner here would
        // double it.
        status={anyMessagePartsAreRunning ? "running" : "complete"}
        // Tools done but the label still settling: no check yet — the line is
        // about to change, and a completion mark on a phrase that then rewrites
        // itself reads as a glitch. The shimmer carries "not final".
        icon={
          !anyMessagePartsAreRunning && pending ? (
            <span className="size-4 shrink-0" aria-hidden />
          ) : undefined
        }
        // Shimmer while the tools run and while the label is still settling (an
        // enriched summary in flight after completion / on a material change).
        titleShimmer={running}
        defaultExpanded={defaultExpanded}
      >
        {children}
      </ToolUIGroup>
    </div>
  );
};
