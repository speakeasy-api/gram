import { useEffect, useRef, useState } from "react";
import { Loader2 } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useConfettiBurst } from "@/components/icon-confetti";
import { Badge } from "@/components/ui/Badge";
import { useIsActiveJourneyStep } from "./journey-steps";
import {
  MAX_EVENTS_SHOWN,
  SOURCE_ICONS,
  sourceLabel,
  type TrafficActivity,
} from "./traffic-activity";

// Each event row occupies this many pixels. Rows are absolutely positioned so
// the slide-down animation translates every row by exactly this much in one
// synchronized tween — no document reflow involved.
const ROW_HEIGHT = 32;

function relativeTime(nowMs: number, timeMs: number): string {
  const diffSec = Math.max(0, Math.round((nowMs - timeMs) / 1000));
  if (diffSec < 60) return `${diffSec}s ago`;
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.floor(diffMin / 60);
  return `${diffHr}h ago`;
}

/** Waiting/Confirmed marker for the step heading. */
export function TrafficBadge({
  hasEvents,
}: {
  hasEvents: boolean;
}): JSX.Element {
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
                        {ev.actor || sourceLabel(ev.source)}
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
