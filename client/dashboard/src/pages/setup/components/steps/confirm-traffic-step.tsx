import { useEffect, useMemo, useRef, useState } from "react";
import {
  Activity,
  AlertTriangle,
  Check,
  Loader2,
  PartyPopper,
} from "lucide-react";
import { useVerifyOnboardingHooksSetup } from "@gram/client/react-query";
import type { OnboardingHookEvent } from "@gram/client/models/components";
import { StepContainer } from "../step-container";
import { cn } from "@/lib/utils";

interface ConfirmTrafficStepProps {
  onComplete: () => void;
  onBack: () => void;
}

const POLL_INTERVAL_MS = 2000;
const MAX_EVENTS_SHOWN = 20;

function sourceLabel(source: string): string {
  switch (source) {
    case "claude_code":
      return "Claude Code";
    case "cursor":
      return "Cursor";
    case "codex":
      return "Codex";
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
}: ConfirmTrafficStepProps) {
  // Wizard session starts now — only count events that arrive after configuration.
  const sessionStartNanoRef = useRef<string>(
    String(BigInt(Date.now()) * 1_000_000n),
  );
  const [cursor, setCursor] = useState<string>(sessionStartNanoRef.current);
  const [events, setEvents] = useState<OnboardingHookEvent[]>([]);
  const [totalReceived, setTotalReceived] = useState(0);
  const [now, setNow] = useState(Date.now());

  // Tick "now" every second so relative timestamps stay fresh.
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  const query = useVerifyOnboardingHooksSetup(
    { sinceUnixNano: cursor },
    undefined,
    { refetchInterval: POLL_INTERVAL_MS },
  );

  // Accumulate new events as polls return them and advance the cursor.
  useEffect(() => {
    if (!query.data) return;
    const data = query.data;
    if (data.events.length === 0) return;
    setEvents((prev) =>
      [...data.events, ...prev].slice(0, MAX_EVENTS_SHOWN * 2),
    );
    setTotalReceived((prev) => prev + data.events.length);
    if (data.latestUnixNano && data.latestUnixNano !== "0") {
      setCursor(data.latestUnixNano);
    }
  }, [query.data]);

  const initialLoading =
    query.isLoading && events.length === 0 && totalReceived === 0;
  const hasEvents = totalReceived > 0;

  const displayed = useMemo(() => events.slice(0, MAX_EVENTS_SHOWN), [events]);

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
      description="We're polling Gram for new hook events. Trigger any action in Claude Code, Cursor, or Codex on a managed machine to confirm the instrumentation works."
      onContinue={onComplete}
      continueLabel="Go to Dashboard"
      showBack
      onBack={onBack}
    >
      <div className="space-y-6">
        <div
          className={cn(
            "rounded-lg border p-6",
            hasEvents
              ? "border-success/20 bg-success/5"
              : "border-destructive/20 bg-destructive/5",
          )}
        >
          <div className="flex items-center gap-4">
            <div
              className={cn(
                "flex h-14 w-14 items-center justify-center rounded-full",
                hasEvents ? "bg-success" : "bg-destructive",
              )}
            >
              {hasEvents ? (
                <Check className="text-background h-7 w-7" />
              ) : (
                <AlertTriangle className="text-background h-7 w-7" />
              )}
            </div>
            <div>
              <p className="text-foreground text-xl font-semibold">
                {hasEvents
                  ? `${totalReceived} event${totalReceived === 1 ? "" : "s"} received`
                  : "Waiting for first event"}
              </p>
              <p className="text-muted-foreground">
                {hasEvents
                  ? "Hooks are flowing into Gram from your configured agents."
                  : "Start a Claude Code, Cursor, or Codex session to trigger the first hook."}
              </p>
            </div>
          </div>
        </div>

        <div className="border-border bg-card overflow-hidden rounded-lg border">
          <div className="border-border flex items-center justify-between border-b px-4 py-3">
            <span className="text-foreground text-sm font-medium">
              Recent activity
            </span>
            <span className="text-success flex items-center gap-1.5 text-xs">
              <span className="bg-success h-1.5 w-1.5 animate-pulse rounded-full" />
              Live
            </span>
          </div>
          <div className="space-y-3 p-4">
            {displayed.length === 0 ? (
              <p className="text-muted-foreground text-sm">
                No events yet. We're checking every {POLL_INTERVAL_MS / 1000}s.
              </p>
            ) : (
              displayed.map((ev, i) => (
                <div
                  key={`${ev.timeUnixNano}-${i}`}
                  className="flex items-center gap-3 text-sm"
                >
                  <span
                    className={cn(
                      "h-2 w-2 flex-shrink-0 rounded-full",
                      ev.status === "blocked" ? "bg-destructive" : "bg-success",
                    )}
                  />
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
                </div>
              ))
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
