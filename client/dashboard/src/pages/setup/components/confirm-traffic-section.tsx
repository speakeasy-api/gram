import { useEffect, useMemo, useRef, useState } from "react";
import { useVerifyOnboardingHooksSetup } from "@gram/client/react-query/verifyOnboardingHooksSetup.js";
import { useAiDetections } from "@gram/client/react-query/aiDetections.js";
import type { OnboardingHookEvent } from "@gram/client/models/components/onboardinghookevent.js";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { SessionAuditRevoke } from "./session-audit-revoke";
import { StepSection } from "./step-section";
import {
  sourceLabel,
  useTrafficArrivals,
  type TrafficActivity,
} from "./traffic-activity";
import { TrafficActivityPanel, TrafficBadge } from "./traffic-activity-panel";

const POLL_INTERVAL_MS = 2000;

function eventKey(ev: OnboardingHookEvent): string {
  // Composite stable key: nano timestamp + tool name + user — uniquely
  // identifies an event without depending on its position in the array.
  return `${ev.timeUnixNano}|${ev.toolName ?? ""}|${ev.userEmail ?? ""}|${ev.chatId ?? ""}`;
}

function eventAction(ev: OnboardingHookEvent): string {
  if (ev.toolName) return `Tool call: ${ev.toolName}`;
  if (ev.eventName) return ev.eventName;
  return `${sourceLabel(ev.source)} event`;
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
  onConfirmed?: (confirmed: boolean) => void;
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
   * Anthropic admin controls card waits for Claude Code and Cowork while the
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
  onConfirmed,
}: ConfirmTrafficSectionProps): JSX.Element {
  // Only count events that arrive after the admin opened this card.
  const sessionStartNanoRef = useRef<string>(
    String(BigInt(Date.now()) * 1_000_000n),
  );
  const [cursor, setCursor] = useState<string>(sessionStartNanoRef.current);

  const query = useVerifyOnboardingHooksSetup(
    { sinceUnixNano: cursor },
    undefined,
    { refetchInterval: POLL_INTERVAL_MS, throwOnError: false },
  );

  const data = query.data;
  const incoming = useMemo<TrafficActivity[]>(() => {
    if (!data || data.events.length === 0) return [];
    return data.events
      .filter((ev) => !matchesSource || matchesSource(ev.source))
      .map((ev) => ({
        key: eventKey(ev),
        source: ev.source,
        actor: ev.userEmail ?? undefined,
        action: eventAction(ev),
        timeMs: Number(BigInt(ev.timeUnixNano) / 1_000_000n),
      }));
  }, [data, matchesSource]);

  const { events, hasEvents } = useTrafficArrivals(incoming);

  useEffect(() => {
    onConfirmed?.(hasEvents);
  }, [hasEvents, onConfirmed]);

  // Advance the cursor past everything the poll returned, matching or not, so
  // filtered-out events aren't refetched on every tick. Declared after the
  // arrivals hook so this batch is already banked when the cursor moves: the
  // move changes the query key, and the next render has no data under it.
  useEffect(() => {
    if (!data || data.events.length === 0) return;
    if (data.latestUnixNano && data.latestUnixNano !== "0") {
      setCursor(data.latestUnixNano);
    }
  }, [data]);

  return (
    <StepSection
      index={index}
      slug="confirm-traffic"
      title="Confirm traffic"
      description={description}
      complete={hasEvents}
      aside={<TrafficBadge hasEvents={hasEvents} />}
    >
      <div className="space-y-4">
        <DetectedClients matchesSource={matchesSource} />
        {callout ? (
          <Alert variant="info" alignTop>
            <AlertTitle>{callout.title}</AlertTitle>
            <AlertDescription>{callout.body}</AlertDescription>
          </Alert>
        ) : null}
        <TrafficActivityPanel
          index={index}
          events={events}
          hasEvents={hasEvents}
          pollIntervalMs={POLL_INTERVAL_MS}
          isError={query.isError}
          onRetry={() => void query.refetch()}
        />
        {hasEvents ? <SessionAuditRevoke /> : null}
      </div>
    </StepSection>
  );
}
