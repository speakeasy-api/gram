"use client";

import { useAssistantState } from "@assistant-ui/react";
import { type FC, useEffect, useMemo, useState } from "react";

import { cn } from "#elements/lib/utils";

/**
 * Whimsical verbs cycled while the assistant is "thinking" — i.e. the message
 * is running but no answer text has streamed in yet (or it is waiting on a tool
 * call / reasoning). The word shown is cosmetic and NOT tied to the actual work
 * being done, mirroring the playful verbs Claude shows while it works. To make
 * them reflect the real action instead, map from the streamed tool name in
 * `ThinkingIndicator` below.
 */
const THINKING_VERBS = [
  "Thinking",
  "Reasoning",
  "Pondering",
  "Synthesizing",
  "Connecting",
  "Analyzing",
  "Inspecting",
  "Correlating",
  "Digging",
  "Untangling",
  "Assembling",
  "Sketching",
  "Reticulating",
  "Wrangling",
  "Distilling",
  "Surveying",
  "Mapping",
  "Cross-referencing",
  "Investigating",
  "Considering",
  "Formulating",
  "Piecing",
  "Tracing",
  "Computing",
  "Noodling",
  "Percolating",
  "Marshalling",
  "Crunching",
  "Orchestrating",
  "Composing",
  "Ruminating",
  "Cogitating",
  "Deliberating",
  "Brewing",
  "Simmering",
  "Mulling",
  "Parsing",
  "Sifting",
  "Scouring",
  "Combing",
  "Sleuthing",
  "Probing",
  "Excavating",
  "Unraveling",
  "Threading",
  "Stitching",
  "Weaving",
  "Charting",
  "Plotting",
  "Calibrating",
  "Tabulating",
  "Aggregating",
  "Collating",
  "Triangulating",
  "Reconciling",
  "Conjuring",
  "Divining",
  "Indexing",
  "Querying",
  "Scanning",
  "Spelunking",
  "Tinkering",
  "Whittling",
  "Churning",
  "Gathering",
  "Decoding",
  "Pattern-matching",
  "Joining the dots",
  "Connecting the dots",
  "Following the trail",
  "Chasing leads",
  "Triple-checking",
] as const;

// Cadence: hold each verb, then crossfade to the next.
const HOLD_MS = 2000;
const FADE_MS = 350;

function shuffled<T>(items: readonly T[]): T[] {
  const copy = [...items];
  for (let i = copy.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [copy[i], copy[j]] = [copy[j]!, copy[i]!];
  }
  return copy;
}

/** Tracks the user's `prefers-reduced-motion` setting, updating if it changes. */
function usePrefersReducedMotion(): boolean {
  const query = "(prefers-reduced-motion: reduce)";
  const [reduced, setReduced] = useState(
    () =>
      typeof window !== "undefined" &&
      typeof window.matchMedia === "function" &&
      window.matchMedia(query).matches,
  );

  useEffect(() => {
    if (
      typeof window === "undefined" ||
      typeof window.matchMedia !== "function"
    )
      return;
    const mql = window.matchMedia(query);
    const onChange = () => setReduced(mql.matches);
    mql.addEventListener("change", onChange);
    return () => mql.removeEventListener("change", onChange);
  }, []);

  return reduced;
}

/**
 * Cycles through a shuffled copy of {@link THINKING_VERBS} while `active`, with
 * a brief opacity dip between words so they crossfade rather than snap. When the
 * user prefers reduced motion, a single verb is shown statically — no timer, no
 * fade — so motion-sensitive users don't get text changing under them.
 */
function useThinkingVerb(
  active: boolean,
  reduced: boolean,
): { verb: string; visible: boolean } {
  const order = useMemo(() => shuffled(THINKING_VERBS), []);
  const [index, setIndex] = useState(0);
  const [visible, setVisible] = useState(true);

  useEffect(() => {
    // Restore visibility on every (de)activation and motion-preference change —
    // only the cycling path below dims it. This runs before the early return so
    // that flipping reduced-motion on (or deactivating) during a fade-out can't
    // latch the verb at opacity-0 forever.
    setVisible(true);

    if (!active || reduced) return;

    let outTimer: ReturnType<typeof setTimeout>;
    let inTimer: ReturnType<typeof setTimeout>;

    const tick = () => {
      setVisible(false);
      outTimer = setTimeout(() => {
        setIndex((i) => (i + 1) % order.length);
        setVisible(true);
        inTimer = setTimeout(tick, HOLD_MS);
      }, FADE_MS);
    };

    inTimer = setTimeout(tick, HOLD_MS);
    return () => {
      clearTimeout(outTimer);
      clearTimeout(inTimer);
    };
  }, [active, reduced, order.length]);

  return { verb: order[index]!, visible };
}

/**
 * Shows a rainbow spinner alongside a cycling "thinking" verb whenever the
 * assistant is running but has not yet produced visible answer text. Once text
 * starts streaming, this hides and the inline trailing dot (see the
 * `aui-md[data-status="running"]` rules in global.css) takes over.
 */
export const ThinkingIndicator: FC = () => {
  const active = useAssistantState(({ message }) => {
    if (message.status?.type !== "running") return false;
    const parts = message.parts;
    if (parts.length === 0) return true;
    const last = parts[parts.length - 1];
    if (!last) return true;
    if (last.type === "tool-call" || last.type === "reasoning") return true;
    if (last.type === "text") return last.text.trim().length === 0;
    return false;
  });

  const reducedMotion = usePrefersReducedMotion();
  const { verb, visible } = useThinkingVerb(active, reducedMotion);

  if (!active) return null;

  return (
    <div
      className="aui-thinking mt-2 flex items-center gap-2 text-sm text-muted-foreground"
      role="status"
    >
      {/* Stable label for assistive tech — the rotating verb below is purely
          decorative, so announcing each word would just be noise. */}
      <span className="sr-only">Assistant is working…</span>
      <span className="aui-thinking-dot" aria-hidden="true" />
      <span
        aria-hidden="true"
        className={cn(
          "aui-thinking-label transition-opacity duration-300 ease-out motion-reduce:transition-none",
          visible ? "opacity-100" : "opacity-0",
        )}
      >
        {verb}…
      </span>
    </div>
  );
};
