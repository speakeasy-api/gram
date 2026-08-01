import { useEffect, useMemo, useRef, useState } from "react";
import { useElements } from "@/elements/hooks/useElements";
import {
  describeToolActivity,
  type HeuristicToolCall,
} from "@/elements/lib/toolActivitySummary";
import type { ToolActivityCall } from "@/elements/types";

/** How long to wait after the tool set stops changing before summarizing. */
const SUMMARIZE_DEBOUNCE_MS = 400;
/** Cap per-call arguments sent over the wire; the server bounds them again. */
const MAX_ARGUMENT_CHARS = 1000;

export interface ToolActivitySummary {
  /** The label to show as the tool-group header. */
  label: string;
  /** True once an LLM-enriched summary has replaced the heuristic. */
  enriched: boolean;
}

export interface UseToolActivitySummaryInput {
  toolCalls: ToolActivityCall[];
  inProgress: boolean;
  userMessage?: string;
}

/**
 * useToolActivitySummary renders a turn's tool activity as a short, human
 * readable "task" label — the Claude-mobile-style line shown in place of a
 * mechanical "Calling N tools" header.
 *
 * It shows an instant heuristic label immediately, then — when the host app
 * provides `config.tools.summarizeToolActivity` — swaps in an LLM-generated
 * summary once it resolves. The enriched label is cached per (tool-set, phase)
 * so the running→complete transition costs at most one extra call, and the last
 * enriched label is retained across transitions so the header never flickers
 * back to the mechanical text.
 */
export function useToolActivitySummary({
  toolCalls,
  inProgress,
  userMessage,
}: UseToolActivitySummaryInput): ToolActivitySummary {
  const { config } = useElements();
  const summarizer = config.tools?.summarizeToolActivity;

  // A stable key over what actually changes the summary: the ordered tool names
  // and the running/complete phase. Argument churn during streaming is
  // deliberately excluded so we don't re-summarize on every token.
  const key = useMemo(
    () => `${toolCalls.map((call) => call.name).join("|")}::${inProgress}`,
    [toolCalls, inProgress],
  );

  const heuristic = useMemo(
    () => describeToolActivity(toolCalls as HeuristicToolCall[], inProgress),
    [toolCalls, inProgress],
  );

  const [enrichedByKey, setEnrichedByKey] = useState<Record<string, string>>(
    {},
  );

  // Latest render inputs, read inside the debounced effect so it doesn't need
  // to depend on (and re-fire for) unstable array/string identities.
  const inputRef = useRef({ toolCalls, userMessage, inProgress });
  inputRef.current = { toolCalls, userMessage, inProgress };

  const enrichedRef = useRef(enrichedByKey);
  enrichedRef.current = enrichedByKey;

  // Only summarize turns we've actually watched run — i.e. live turns. A turn
  // that mounts already-complete (an old conversation loaded from history)
  // keeps its heuristic label so browsing history costs no model calls.
  const wasRunningRef = useRef(false);
  if (inProgress) {
    wasRunningRef.current = true;
  }

  useEffect(() => {
    if (!summarizer) return;
    if (!wasRunningRef.current) return;
    if (enrichedRef.current[key]) return;

    const { toolCalls, userMessage, inProgress } = inputRef.current;
    if (toolCalls.length === 0) return;

    const controller = new AbortController();
    const timer = setTimeout(() => {
      void (async () => {
        try {
          const summary = await summarizer({
            toolCalls: toolCalls.map((call) => ({
              name: call.name,
              arguments: call.arguments?.slice(0, MAX_ARGUMENT_CHARS),
            })),
            userMessage,
            inProgress,
            signal: controller.signal,
          });
          if (controller.signal.aborted) return;
          const trimmed = summary?.trim();
          if (trimmed) {
            setEnrichedByKey((prev) => ({ ...prev, [key]: trimmed }));
          }
        } catch {
          // Swallow — the heuristic label is a fine fallback.
        }
      })();
    }, SUMMARIZE_DEBOUNCE_MS);

    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [key, summarizer]);

  // Retain the most recent enriched label so a phase/tool-set change doesn't
  // briefly regress the header to the mechanical heuristic while the next
  // summary is in flight.
  const lastEnrichedRef = useRef<string | null>(null);
  const current = enrichedByKey[key];
  if (current) {
    lastEnrichedRef.current = current;
  }

  const enrichedLabel = current ?? lastEnrichedRef.current;
  if (!summarizer || !enrichedLabel) {
    return { label: heuristic, enriched: false };
  }
  return { label: enrichedLabel, enriched: true };
}
