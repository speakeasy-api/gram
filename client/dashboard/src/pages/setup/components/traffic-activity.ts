import { useEffect, useRef, useState } from "react";

/** Rows the live tail holds. The panel sizes its window to the same number. */
export const MAX_EVENTS_SHOWN = 8;
// Spacing between visible event arrivals. When a poll returns multiple new
// events, we queue them and play one in every PLAYBACK_INTERVAL_MS so the
// sliding-window animation reads cleanly (1 in, 1 out) instead of a cascade.
const PLAYBACK_INTERVAL_MS = 400;
// An organization busier than the playback rate would otherwise queue arrivals
// faster than they drain, so the tail would fall further behind for as long as
// the card stayed open. Past this many pending, the oldest are dropped: the
// panel is a sign of life, not a log, and a stale row is worth less than a
// current one.
const MAX_QUEUED = MAX_EVENTS_SHOWN * 4;
// Keys are only needed to recognise a repeat while it is still being listed.
// Holding every key ever seen would grow without bound on a long-lived card.
const MAX_KEYS_REMEMBERED = 500;

export const SOURCE_ICONS: Record<string, string> = {
  "claude-code": "/icons/platforms/claude.svg",
  "claude-code-desktop": "/icons/platforms/claude.svg",
  // Inference hook deliveries: Claude conversations on claude.ai and Claude
  // Code's web surface, both reported by Anthropic rather than by a plugin.
  "claude-chat-web": "/icons/platforms/claude.svg",
  "claude-code-web": "/icons/platforms/claude.svg",
  cowork: "/icons/platforms/claude.svg",
  cursor: "/icons/platforms/cursor.svg",
  codex: "/icons/platforms/openai.svg",
  chatgpt: "/icons/platforms/openai.svg",
  "chatgpt-work": "/icons/platforms/openai.svg",
};

export function sourceLabel(source: string): string {
  switch (source) {
    case "claude-code":
      return "Claude Code";
    case "claude-code-desktop":
      return "Claude Desktop";
    case "claude-chat-web":
      return "Claude Chat Web";
    case "claude-code-web":
      return "Claude Code Web";
    case "cursor":
      return "Cursor";
    case "codex":
      return "Codex";
    case "chatgpt":
      return "ChatGPT";
    case "chatgpt-work":
      return "ChatGPT Work";
    case "cowork":
    case "claude-cowork":
      return "Cowork";
    default:
      return source;
  }
}

/**
 * One line in the live tail, whatever produced it. Hook events and inference
 * hook conversations reach the panel through different reads, so they are
 * flattened to this before it sees them.
 */
export interface TrafficActivity {
  /** Stable identity, so a repeated poll doesn't replay the same row. */
  key: string;
  /** Product surface, for the icon and the fallback label. */
  source: string;
  /** Who produced it, e.g. an email. Falls back to the source's label. */
  actor?: string;
  /** What happened, e.g. "Tool call: Bash". */
  action: string;
  timeMs: number;
}

/**
 * Accumulates whatever the caller's poll turned up into the visible window,
 * one arrival at a time. Activities already seen are ignored, so a feed that
 * re-reports the same rows (a listing rather than a cursor) is safe to pass in
 * whole on every tick.
 */
export function useTrafficArrivals(incoming: TrafficActivity[]): {
  events: TrafficActivity[];
  hasEvents: boolean;
} {
  const [events, setEvents] = useState<TrafficActivity[]>([]);
  const [totalReceived, setTotalReceived] = useState(0);
  // Pending playback queue — new activities land here and drain into `events`
  // one at a time via the playback interval.
  const queueRef = useRef<TrafficActivity[]>([]);
  const seenKeysRef = useRef<Set<string>>(new Set());

  // Drain the playback queue one event at a time so the sliding window
  // animation can be observed per arrival, even when bursts deliver many at
  // once.
  useEffect(() => {
    const id = setInterval(() => {
      if (queueRef.current.length === 0) return;
      const next = queueRef.current.shift();
      if (!next) return;
      setEvents((prev) => [next, ...prev].slice(0, MAX_EVENTS_SHOWN));
    }, PLAYBACK_INTERVAL_MS);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    const fresh = incoming.filter((activity) => {
      if (seenKeysRef.current.has(activity.key)) return false;
      seenKeysRef.current.add(activity.key);
      return true;
    });
    if (fresh.length === 0) return;
    // Insertion-ordered, so the oldest keys are the first to fall out.
    while (seenKeysRef.current.size > MAX_KEYS_REMEMBERED) {
      const oldest = seenKeysRef.current.values().next();
      if (oldest.done) break;
      seenKeysRef.current.delete(oldest.value);
    }
    // Feeds return newest-first; reverse so we enqueue oldest-first and the
    // newest event still ends up at the top of the visible stack.
    queueRef.current.push(...[...fresh].reverse());
    if (queueRef.current.length > MAX_QUEUED) {
      queueRef.current = queueRef.current.slice(-MAX_QUEUED);
    }
    setTotalReceived((prev) => prev + fresh.length);
  }, [incoming]);

  return { events, hasEvents: totalReceived > 0 };
}
