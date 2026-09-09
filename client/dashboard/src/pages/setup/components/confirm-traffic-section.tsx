import { useEffect, useRef, useState } from "react";
import { Loader2 } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useVerifyOnboardingHooksSetup } from "@gram/client/react-query/verifyOnboardingHooksSetup.js";
import { useAiDetections } from "@gram/client/react-query/aiDetections.js";
import type { OnboardingHookEvent } from "@gram/client/models/components/onboardinghookevent.js";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { StepSection } from "./step-section";

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
  chatgpt: "/icons/platforms/openai.svg",
  "chatgpt-work": "/icons/platforms/openai.svg",
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

// Claude Code, Cowork and Chat all arrive through the one Claude install, so
// the Anthropic card's chip names the vendor rather than the harness the
// detector happened to match.
const CLIENT_LABELS: Record<string, string> = {
  "claude-code": "Claude",
};

// The clients this card covers that the device agent has actually seen on an
// enrolled machine, so the prompt names real tools instead of "a coding
// assistant". Harnesses only: opening Ollama or LM Studio produces no hook
// traffic. Detections are org-admin-only, so a task page opened by an
// assignee simply gets no cards and keeps the generic prompt.
function DetectedClients({
  matchesSource,
}: {
  matchesSource?: (source: string) => boolean;
}): JSX.Element | null {
  const { data } = useAiDetections({ category: "harness" }, undefined, {
    throwOnError: false,
    retry: false,
  });

  const clients = (data?.detections ?? []).filter(
    (detection) => !matchesSource || matchesSource(detection.targetId),
  );
  if (clients.length === 0) return null;

  return (
    <div className="space-y-2">
      <p className="text-foreground text-sm font-medium">
        Open one of these clients:
      </p>
      <div className="flex flex-wrap gap-2">
        {clients.map((client) => (
          <span
            key={client.targetId}
            className="border-border bg-card flex items-center gap-2 border px-3 py-2"
          >
            <AgentProviderIcon
              source={client.targetId}
              className="h-4 w-4 flex-shrink-0"
            />
            <span className="text-foreground text-sm">
              {CLIENT_LABELS[client.targetId] ?? client.displayName}
            </span>
          </span>
        ))}
      </div>
      <p className="text-muted-foreground text-xs">
        Detected by the device agent on your enrolled machines.
      </p>
    </div>
  );
}

interface ConfirmTrafficSectionProps {
  index: number;
  /** What the admin should do to make an event show up. */
  description: string;
  /**
   * Called out under the client list, for a card whose clients report nothing
   * until something is switched on. Kept out of `description` so the step's
   * opening line stays a summary and the precondition reads as one.
   */
  callout?: { title: string; body: string };
  /**
   * Only events from matching sources count towards confirmation, so the
   * Anthropic observability card waits for Claude Code and Cowork while the
   * other-platforms card waits for everything else. Pass a module-level
   * function: the filter is an effect dependency.
   */
  matchesSource?: (source: string) => boolean;
}

// Live tail of hook events that arrive after this section mounts. It sits at
// the end of the cards that instrument something, so "did it work?" is
// answered in the same place the setup happened rather than on a card of its
// own.
export function ConfirmTrafficSection({
  index,
  description,
  callout,
  matchesSource,
}: ConfirmTrafficSectionProps): JSX.Element {
  // Only count events that arrive after the admin opened this card.
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
    // Advance the cursor past everything the poll returned, matching or not,
    // so filtered-out events aren't refetched on every tick.
    if (data.latestUnixNano && data.latestUnixNano !== "0") {
      setCursor(data.latestUnixNano);
    }
    const fresh = data.events.filter((e) => {
      if (matchesSource && !matchesSource(e.source)) return false;
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
  }, [query.data, matchesSource]);

  const hasEvents = totalReceived > 0;

  return (
    <StepSection
      index={index}
      title="Confirm traffic"
      description={description}
      complete={hasEvents}
      aside={
        hasEvents ? (
          <Badge variant="success" background>
            <Badge.Text>Confirmed</Badge.Text>
          </Badge>
        ) : (
          <Badge variant="neutral" background>
            <Badge.Text>Waiting</Badge.Text>
          </Badge>
        )
      }
    >
      <div className="space-y-4">
        <DetectedClients matchesSource={matchesSource} />
        {callout ? (
          <Alert variant="info" alignTop>
            <AlertTitle>{callout.title}</AlertTitle>
            <AlertDescription>{callout.body}</AlertDescription>
          </Alert>
        ) : null}
        {query.isError ? (
          <div
            role="alert"
            className="border-destructive text-destructive border p-4 text-sm"
          >
            <p>We couldn't check for traffic. Try again.</p>
            <button
              type="button"
              className="mt-2 underline"
              onClick={() => void query.refetch()}
            >
              Retry
            </button>
          </div>
        ) : null}
        <div className="border-border bg-card overflow-hidden border">
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
                  {events.map((ev, i) => (
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

        {hasEvents ? (
          <p className="border-success-default text-default-success border p-3 text-sm">
            Traffic confirmed. Events are reaching Speakeasy from what you just
            set up.
          </p>
        ) : null}
      </div>
    </StepSection>
  );
}
