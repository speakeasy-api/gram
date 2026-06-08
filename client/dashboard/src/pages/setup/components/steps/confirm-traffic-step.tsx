import { useEffect, useRef, useState } from "react";
import { Activity, Loader2, PartyPopper } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useVerifyOnboardingHooksSetup } from "@gram/client/react-query";
import type { OnboardingHookEvent } from "@gram/client/models/components";
import { StepContainer } from "../step-container";

interface ConfirmTrafficStepProps {
  onComplete: () => void;
  onBack: () => void;
}

const POLL_INTERVAL_MS = 2000;
const MAX_EVENTS_SHOWN = 8;
// Spacing between visible event arrivals. When a poll returns multiple new
// events, we queue them and play one in every PLAYBACK_INTERVAL_MS so the
// sliding-window animation reads cleanly (1 in, 1 out) instead of a cascade.
const PLAYBACK_INTERVAL_MS = 400;
// Each event row occupies this many pixels. Rows are absolutely positioned so
// the slide-down animation translates every row by exactly this much in one
// synchronized tween — no document reflow involved.
const ROW_HEIGHT = 32;

const SOURCE_ICONS: Record<string, string> = {
  "claude-code": "/icons/platforms/claude.svg",
  "claude-code-desktop": "/icons/platforms/claude.svg",
  cowork: "/icons/platforms/claude.svg",
  cursor: "/icons/platforms/cursor.svg",
  codex: "/icons/platforms/openai.svg",
};

function eventKey(ev: OnboardingHookEvent): string {
  // Composite stable key: nano timestamp + tool name + user — uniquely
  // identifies an event without depending on its position in the array.
  return `${ev.timeUnixNano}|${ev.toolName ?? ""}|${ev.userEmail ?? ""}|${ev.chatId ?? ""}`;
}

function sourceLabel(source: string): string {
  switch (source) {
    case "claude-code":
      return "Claude Code";
    case "claude-code-desktop":
      return "Claude Desktop";
    case "cursor":
      return "Cursor";
    case "codex":
      return "Codex";
    case "cowork":
      return "Cowork";
    default:
      return source;
  }
}

function eventAction(ev: OnboardingHookEvent): string {
  if (ev.toolName) return `Tool call: ${ev.toolName}`;
  if (ev.eventName) return ev.eventName;
  return `${sourceLabel(ev.source)} event`;
}

function relativeTime(nowMs: number, timeUnixNano: string): string {
  const tNs = BigInt(timeUnixNano);
  const tMs = Number(tNs / 1_000_000n);
  const diffSec = Math.max(0, Math.round((nowMs - tMs) / 1000));
  if (diffSec < 60) return `${diffSec}s ago`;
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.floor(diffMin / 60);
  return `${diffHr}h ago`;
}

export function ConfirmTrafficStep({
  onComplete,
  onBack,
}: ConfirmTrafficStepProps): JSX.Element {
  // Wizard session starts now — only count events that arrive after configuration.
  const sessionStartNanoRef = useRef<string>(
    String(BigInt(Date.now()) * 1_000_000n),
  );
  const [cursor, setCursor] = useState<string>(sessionStartNanoRef.current);
  const [events, setEvents] = useState<OnboardingHookEvent[]>([]);
  const [totalReceived, setTotalReceived] = useState(0);
  const [now, setNow] = useState(Date.now());
  // Pending playback queue — newest events from polls land here and drain into
  // `events` one at a time via the playback interval.
  const queueRef = useRef<OnboardingHookEvent[]>([]);
  const seenKeysRef = useRef<Set<string>>(new Set());

  // Tick "now" every second so relative timestamps stay fresh.
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

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

  const query = useVerifyOnboardingHooksSetup(
    { sinceUnixNano: cursor },
    undefined,
    { refetchInterval: POLL_INTERVAL_MS },
  );

  // Enqueue new events from each poll. Oldest of the batch is played back
  // first so visual order matches arrival order (newest still ends up at top).
  useEffect(() => {
    if (!query.data) return;
    const data = query.data;
    if (data.events.length === 0) return;
    const fresh = data.events.filter((e) => {
      const k = eventKey(e);
      if (seenKeysRef.current.has(k)) return false;
      seenKeysRef.current.add(k);
      return true;
    });
    if (fresh.length === 0) return;
    // API returns newest-first; reverse so we enqueue oldest-first and the
    // newest event still ends up at the top of the visible stack.
    queueRef.current.push(...[...fresh].reverse());
    setTotalReceived((prev) => prev + fresh.length);
    if (data.latestUnixNano && data.latestUnixNano !== "0") {
      setCursor(data.latestUnixNano);
    }
  }, [query.data]);

  const initialLoading =
    query.isLoading && events.length === 0 && totalReceived === 0;
  const hasEvents = totalReceived > 0;

  // FIFO ring: newest at top, capped at MAX_EVENTS_SHOWN. Eviction is driven
  // by new arrivals (state-level slice), not by age.
  const displayed = events;

  if (initialLoading) {
    return (
      <StepContainer
        icon={
          <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
            <Activity className="text-foreground h-6 w-6" />
          </div>
        }
        title="Verifying traffic"
        description="Waiting for the first hook event to arrive from your configured agents…"
        onContinue={() => {}}
        showBack
        onBack={onBack}
        canContinue={false}
        isLoading
      >
        <div className="flex flex-col items-center justify-center py-16">
          <div className="relative mb-6">
            <div className="bg-foreground/10 absolute inset-0 animate-ping rounded-full" />
            <div className="bg-secondary relative flex h-16 w-16 items-center justify-center rounded-full">
              <Loader2 className="text-foreground h-8 w-8 animate-spin" />
            </div>
          </div>
          <p className="text-muted-foreground text-sm">
            Listening for Claude Code, Cursor, and Codex hooks…
          </p>
        </div>
      </StepContainer>
    );
  }

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
          <Activity className="text-foreground h-6 w-6" />
        </div>
      }
      title="Confirm traffic"
      description="We're listening for events from your agent platforms. Trigger any action in Claude Code, Cursor, or Codex on a managed machine to confirm the instrumentation works."
      onContinue={onComplete}
      continueLabel="Complete setup"
      showBack
      onBack={onBack}
    >
      <div className="space-y-6">
        <div className="border-border bg-card overflow-hidden rounded-lg border">
          <div className="border-border flex items-center justify-between border-b px-4 py-3">
            <span className="text-foreground text-sm font-medium">
              Recent activity
            </span>
            <span className="flex items-center gap-2 text-xs font-medium text-emerald-600">
              <span className="relative flex h-2.5 w-2.5">
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-500 opacity-75" />
                <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-emerald-500" />
              </span>
              Live tail
            </span>
          </div>
          <div className="px-4 py-2">
            {displayed.length === 0 ? (
              <div
                className="flex flex-col items-center justify-center gap-3"
                style={{ height: ROW_HEIGHT * MAX_EVENTS_SHOWN }}
              >
                <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
                <p className="text-muted-foreground text-sm">
                  No events yet. We're checking every {POLL_INTERVAL_MS / 1000}
                  s.
                </p>
              </div>
            ) : (
              <div
                className="relative overflow-hidden"
                style={{ height: ROW_HEIGHT * MAX_EVENTS_SHOWN }}
              >
                <AnimatePresence initial={false}>
                  {displayed.map((ev, i) => (
                    <motion.div
                      key={eventKey(ev)}
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
                          {ev.userEmail ?? sourceLabel(ev.source)}
                        </span>
                        <span className="text-muted-foreground">
                          {" "}
                          - {eventAction(ev)}
                        </span>
                      </span>
                      <span className="text-muted-foreground flex-shrink-0 text-xs">
                        {relativeTime(now, ev.timeUnixNano)}
                      </span>
                    </motion.div>
                  ))}
                </AnimatePresence>
              </div>
            )}
          </div>
        </div>

        {hasEvents && (
          <div className="bg-foreground/5 border-foreground/10 rounded-lg border p-4">
            <div className="flex items-start gap-3">
              <div className="bg-foreground mt-0.5 flex h-8 w-8 flex-shrink-0 items-center justify-center rounded">
                <PartyPopper className="text-background h-4 w-4" />
              </div>
              <div>
                <p className="text-foreground text-sm font-medium">
                  Setup complete!
                </p>
                <p className="text-muted-foreground mt-1 text-sm">
                  Your organization is receiving hook events from agent
                  platforms.
                </p>
              </div>
            </div>
          </div>
        )}
      </div>
    </StepContainer>
  );
}
