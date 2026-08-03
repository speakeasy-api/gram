import { useEffect, useMemo, useRef, useState } from "react";
import { useElements } from "@/elements/hooks/useElements";
import {
  describeToolActivity,
  type HeuristicToolCall,
} from "@/elements/lib/toolActivitySummary";
import type { ToolActivityCall } from "@/elements/types";

/** How long to wait after the tool set stops changing before summarizing. */
const SUMMARIZE_DEBOUNCE_MS = 400;
/** Cap per-call arguments sent over the wire; the endpoint rejects longer. */
const MAX_ARGUMENT_CHARS = 600;
/** Cap the user prompt sent over the wire; the endpoint rejects longer. */
const MAX_USER_MESSAGE_CHARS = 2000;
/** Cap how many calls are sent; the server summarizes at most this many. */
const MAX_TOOL_CALLS = 20;
/** Abort a summarization request that hasn't resolved, so `pending` clears. */
const REQUEST_TIMEOUT_MS = 15000;

export interface ToolActivitySummary {
  /** The label to show as the tool-group header. */
  label: string;
  /** True once an LLM-enriched summary has replaced the heuristic. */
  enriched: boolean;
  /**
   * True while the label for the current activity is still being determined —
   * an enriched summary is debouncing or in flight. Drives the header shimmer
   * so the line reads as "still settling", not final.
   */
  pending: boolean;
}

export interface UseToolActivitySummaryInput {
  toolCalls: ToolActivityCall[];
  inProgress: boolean;
  /**
   * True while the turn this group belongs to is still in flight. Distinct from
   * `inProgress`, which tracks this group's own tool parts: a turn whose tools
   * run server-side streams them in already-complete, so `inProgress` can be
   * false for a group that is very much part of the live turn. Enrichment is
   * gated on this, not on `inProgress`, so those turns still get a summary.
   * @default false
   */
  isLiveTurn?: boolean;
  userMessage?: string;
  /**
   * When false, skip enrichment entirely (heuristic only). Used for tool groups
   * rendered by a custom component, which never display this label — no reason
   * to spend a model call on them.
   * @default true
   */
  enabled?: boolean;
}

/**
 * useToolActivitySummary renders a turn's tool activity as a short, human
 * readable "task" label — the Claude-mobile-style line shown in place of a
 * mechanical "Calling N tools" header.
 *
 * It shows an instant heuristic label immediately, then — when the host app
 * provides `config.tools.summarizeToolActivity` — swaps in an LLM-generated
 * summary once it resolves. The enriched label is cached per (tool-set, phase,
 * prompt), so the running→complete transition costs at most one extra call.
 *
 * As more tool calls arrive within the same turn (no interleaved agent text),
 * the summary re-generates. While the *nature* of the work is unchanged — the
 * same set of tools, just more calls — the last label is retained to avoid
 * flicker; but when the agent materially shifts what it's doing (the set of
 * distinct tools changes), the stale label is dropped immediately so the header
 * reflects the new activity. Retention is scoped to the running phase, so a
 * completed group never keeps showing a present-tense label if its final
 * summary fails to arrive.
 */
export function useToolActivitySummary({
  toolCalls,
  inProgress,
  isLiveTurn = false,
  userMessage,
  enabled = true,
}: UseToolActivitySummaryInput): ToolActivitySummary {
  const { config } = useElements();
  const summarizer = enabled ? config.tools?.summarizeToolActivity : undefined;

  // The set of distinct tools captures the *nature* of the activity.
  const materialSignature = useMemo(
    () => [...new Set(toolCalls.map((call) => call.name))].sort().join("|"),
    [toolCalls],
  );

  // A key over everything that should change the summary: the ordered tool
  // names, the running/complete phase, and the user's prompt. Argument churn
  // during streaming is deliberately excluded so we don't re-summarize on every
  // token.
  const key = useMemo(
    () =>
      `${toolCalls.map((call) => call.name).join("|")}::${inProgress}::${userMessage ?? ""}`,
    [toolCalls, inProgress, userMessage],
  );

  const heuristic = useMemo(
    () =>
      describeToolActivity(
        toolCalls as HeuristicToolCall[],
        inProgress,
        userMessage,
      ),
    [toolCalls, inProgress, userMessage],
  );

  const [enrichedByKey, setEnrichedByKey] = useState<Record<string, string>>(
    {},
  );
  // Keys whose enrichment attempt has finished (success OR failure), so a failed
  // summary stops the shimmer instead of pending forever.
  const [settledByKey, setSettledByKey] = useState<Record<string, true>>({});
  // The most recent enriched label plus the activity signature it described.
  // Held in state (not a render-written ref) so retention stays consistent under
  // concurrent/StrictMode rendering.
  const [lastEnriched, setLastEnriched] = useState<{
    summary: string;
    signature: string;
  } | null>(null);

  // Latest render inputs, read inside the debounced effect so it doesn't need
  // to depend on (and re-fire for) unstable array/string identities.
  const inputRef = useRef({
    toolCalls,
    userMessage,
    inProgress,
    materialSignature,
  });
  inputRef.current = { toolCalls, userMessage, inProgress, materialSignature };

  const enrichedRef = useRef(enrichedByKey);
  enrichedRef.current = enrichedByKey;

  const settledRef = useRef(settledByKey);
  settledRef.current = settledByKey;

  // Only summarize live turns. A turn that mounts already-complete (an old
  // conversation loaded from history) keeps its heuristic label so browsing
  // history costs no model calls. Liveness is either this group's own parts
  // running or the thread still running — see `isLiveTurn`.
  const wasRunningRef = useRef(false);
  if (inProgress || isLiveTurn) {
    wasRunningRef.current = true;
  }

  useEffect(() => {
    if (!summarizer) return;
    if (!wasRunningRef.current) return;
    // Skip keys we've already enriched or already tried and failed — a failed
    // attempt is terminal, so a summarizer identity change must not retry it.
    if (enrichedRef.current[key]) return;
    if (settledRef.current[key]) return;

    const { toolCalls, userMessage, inProgress, materialSignature } =
      inputRef.current;
    if (toolCalls.length === 0) return;

    const controller = new AbortController();
    const debounceTimer = setTimeout(() => {
      // Bound the request itself: a hung summarizer must still resolve to the
      // heuristic instead of shimmering forever.
      let timedOut = false;
      const requestTimer = setTimeout(() => {
        timedOut = true;
        controller.abort();
      }, REQUEST_TIMEOUT_MS);

      void (async () => {
        let summary: string | null = null;
        try {
          summary = await summarizer({
            // Only the most recent calls matter for the current task, and the
            // server bounds the count too — send a recent window, not the whole
            // (unbounded) turn history.
            toolCalls: toolCalls.slice(-MAX_TOOL_CALLS).map((call) => ({
              name: call.name,
              arguments: call.arguments?.slice(0, MAX_ARGUMENT_CHARS),
            })),
            userMessage: userMessage?.slice(0, MAX_USER_MESSAGE_CHARS),
            inProgress,
            signal: controller.signal,
          });
        } catch {
          // Swallow — the heuristic label is a fine fallback.
          summary = null;
        } finally {
          clearTimeout(requestTimer);
        }
        // Aborted by effect cleanup (superseded key) — drop silently. A
        // timeout-abort is different: let it fall through and settle so the
        // shimmer resolves to the heuristic.
        if (controller.signal.aborted && !timedOut) return;
        const trimmed = summary?.trim();
        if (trimmed) {
          setEnrichedByKey((prev) => ({ ...prev, [key]: trimmed }));
          setLastEnriched({ summary: trimmed, signature: materialSignature });
        }
        // Mark the attempt as finished either way so `pending` (and the header
        // shimmer) resolves even when the summary failed, timed out, or was
        // empty.
        setSettledByKey((prev) =>
          prev[key] ? prev : { ...prev, [key]: true },
        );
      })();
    }, SUMMARIZE_DEBOUNCE_MS);

    return () => {
      clearTimeout(debounceTimer);
      controller.abort();
    };
  }, [key, summarizer]);

  const current = enrichedByKey[key];

  // Retain the last enriched label across pure growth of the same activity to
  // avoid flicker — but only while running. On completion we don't reuse the
  // present-tense label; the past-tense heuristic stands in until the final
  // summary arrives, so a failed completion summary can't leave "Searching…"
  // frozen on a finished group.
  const retained =
    inProgress && lastEnriched?.signature === materialSignature
      ? lastEnriched.summary
      : undefined;

  // Pending while we're actively working toward the label for the current
  // activity: a summarizer is configured, this is a live turn, and the current
  // (tool-set, phase, prompt) has neither produced an enriched summary nor
  // finished trying. Keeps the header shimmering through the post-completion and
  // material-change windows, then stops.
  const pending =
    Boolean(summarizer) &&
    wasRunningRef.current &&
    toolCalls.length > 0 &&
    !current &&
    !settledByKey[key];

  const enrichedLabel = current ?? retained;
  if (!summarizer || !enrichedLabel) {
    return { label: heuristic, enriched: false, pending };
  }
  return { label: enrichedLabel, enriched: true, pending };
}
