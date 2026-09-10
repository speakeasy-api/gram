import { useEffect, useRef, useState } from "react";
import { Loader2 } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useConfettiBurst } from "@/components/icon-confetti";
import { Badge } from "@/components/ui/Badge";
import { useIsActiveJourneyStep } from "./journey-steps";

const MAX_EVENTS_SHOWN = 8;
// Spacing between visible event arrivals. When a poll returns multiple new
// events, we queue them and play one in every PLAYBACK_INTERVAL_MS so the
// sliding-window animation reads cleanly (1 in, 1 out) instead of a cascade.
const PLAYBACK_INTERVAL_MS = 400;
// Each event row occupies this many pixels. Rows are absolutely positioned so
// the slide-down animation translates every row by exactly this much in one
// synchronized tween — no document reflow involved.
const ROW_HEIGHT = 32;

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

function relativeTime(nowMs: number, timeMs: number): string {
  const diffSec = Math.max(0, Math.round((nowMs - timeMs) / 1000));
  if (diffSec < 60) return `${diffSec}s ago`;
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.floor(diffMin / 60);
  return `${diffHr}h ago`;
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
    // Feeds return newest-first; reverse so we enqueue oldest-first and the
    // newest event still ends up at the top of the visible stack.
    queueRef.current.push(...[...fresh].reverse());
    setTotalReceived((prev) => prev + fresh.length);
  }, [incoming]);

  return { events, hasEvents: totalReceived > 0 };
}

/** Waiting/Confirmed marker for the step heading. */
export function trafficBadge(hasEvents: boolean): JSX.Element {
  return hasEvents ? (
    <Badge variant="success" background>
      <Badge.Text>Confirmed</Badge.Text>
    </Badge>
  ) : (
    <Badge variant="neutral" background>
      <Badge.Text>Waiting</Badge.Text>
    </Badge>
  );
}

interface TrafficActivityPanelProps {
  /** The step this panel sits in, for the confetti hold. */
  index: number;
  events: TrafficActivity[];
  hasEvents: boolean;
  /** How often the feed is checked, named in the empty state. */
  pollIntervalMs: number;
  isError: boolean;
  onRetry: () => void;
}

// The live tail itself: a bordered panel that plays each arrival into a
// sliding window, and celebrates the first one. Shared by every card that ends
// in "did it work?", whichever read answers that question for it.
export function TrafficActivityPanel({
  index,
  events,
  hasEvents,
  pollIntervalMs,
  isError,
  onRetry,
}: TrafficActivityPanelProps): JSX.Element {
  const [now, setNow] = useState(Date.now());

  // Tick "now" every second so relative timestamps stay fresh.
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  // The first event is the moment the whole card was working towards, so it
  // gets one burst over the activity panel. Only the first: every event after
  // it is the feature working normally, and a popper on each would turn a
  // milestone into noise.
  //
  // Held until this step is the one on screen. Sections stay mounted while
  // hidden so their polling survives, so an event that lands while the reader
  // is still on an earlier step would otherwise spend the celebration into a
  // hidden canvas and leave nothing for them to arrive to.
  const onScreen = useIsActiveJourneyStep(index);
  const { canvasRef, burst } = useConfettiBurst();
  const celebrated = useRef(false);
  useEffect(() => {
    if (!hasEvents || !onScreen || celebrated.current) return;
    celebrated.current = true;
    burst();
  }, [hasEvents, onScreen, burst]);

  return (
    <>
      {isError ? (
        <div
          role="alert"
          className="border-destructive text-destructive border p-4 text-sm"
        >
          <p>We couldn't check for traffic. Try again.</p>
          <button type="button" className="mt-2 underline" onClick={onRetry}>
            Retry
          </button>
        </div>
      ) : null}
      {/* isolate so the canvas's -z-10 lands between the panel's own
          background and its contents, rather than behind the panel. */}
      <div className="border-border bg-card relative isolate overflow-hidden border">
        {/* Behind the panel's contents and clipped to it: the pieces show
            through between the rows rather than over the text. */}
        <canvas
          ref={canvasRef}
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 -z-10 size-full"
        />
        <div className="border-border flex items-center justify-between border-b px-4 py-3">
          <span className="text-foreground text-sm font-medium">
            Recent activity
          </span>
          <span className="text-default-success flex items-center gap-2 text-xs font-medium">
            <span className="relative flex h-2.5 w-2.5">
              <span className="bg-success-default absolute inline-flex h-full w-full rounded-full opacity-75 motion-safe:animate-ping" />
              <span className="bg-success-default relative inline-flex h-2.5 w-2.5 rounded-full" />
            </span>
            Live tail
          </span>
        </div>
        <div className="px-4 py-2">
          {events.length === 0 ? (
            <div
              className="flex flex-col items-center justify-center gap-3"
              style={{ height: ROW_HEIGHT * MAX_EVENTS_SHOWN }}
            >
              <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
              <p className="text-muted-foreground text-sm">
                No events yet. We're checking every {pollIntervalMs / 1000}s.
              </p>
            </div>
          ) : (
            <div
              className="relative overflow-hidden"
              style={{ height: ROW_HEIGHT * MAX_EVENTS_SHOWN }}
            >
              <AnimatePresence initial={false}>
                {events.map((ev, i) => (
                  <motion.div
                    key={ev.key}
                    initial={{ y: -ROW_HEIGHT, opacity: 0 }}
                    animate={{ y: i * ROW_HEIGHT, opacity: 1 }}
                    exit={{
                      y: MAX_EVENTS_SHOWN * ROW_HEIGHT,
                      opacity: 0,
                    }}
                    transition={{ duration: 0.3, ease: [0.22, 1, 0.36, 1] }}
                    className="absolute inset-x-0 flex items-center gap-3 text-sm"
                    style={{ height: ROW_HEIGHT }}
                  >
                    {SOURCE_ICONS[ev.source] ? (
                      <img
                        src={SOURCE_ICONS[ev.source]}
                        alt={sourceLabel(ev.source)}
                        title={sourceLabel(ev.source)}
                        className="h-4 w-4 flex-shrink-0"
                      />
                    ) : null}
                    <span className="text-foreground flex-1 truncate">
                      <span className="font-medium">
                        {ev.actor ?? sourceLabel(ev.source)}
                      </span>
                      <span className="text-muted-foreground">
                        {" "}
                        - {ev.action}
                      </span>
                    </span>
                    <span className="text-muted-foreground flex-shrink-0 text-xs">
                      {relativeTime(now, ev.timeMs)}
                    </span>
                  </motion.div>
                ))}
              </AnimatePresence>
            </div>
          )}
        </div>
      </div>

      {hasEvents ? (
        <p className="border-success-default text-default-success border p-3 text-sm">
          Traffic confirmed. Events are reaching Speakeasy from what you just
          set up.
        </p>
      ) : null}
    </>
  );
}
