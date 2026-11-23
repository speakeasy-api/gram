"use client";

import { useAuiState } from "@assistant-ui/react";
import { type FC, useEffect, useMemo, useState } from "react";

import { isThinkingIndicatorActive } from "@/elements/lib/thinkingIndicator";
import { THINKING_VERBS } from "@/elements/lib/thinkingVerbs";
import { RainbowSpinner } from "@/components/ui/Spinner";
import { usePrefersReducedMotion } from "@/elements/hooks/usePrefersReducedMotion";
import { cn } from "@/lib/utils";

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
 * assistant is running and no other working signal is on screen — see
 * {@link isThinkingIndicatorActive} for how it hands off to the streaming-text
 * dot and the tool and reasoning shimmers.
 */
export const ThinkingIndicator: FC = () => {
  const active = useAuiState(({ message }) =>
    isThinkingIndicatorActive(message),
  );

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
      <RainbowSpinner className="size-3.5" />
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
