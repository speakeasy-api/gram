import { useCallback, useRef, useState, type JSX } from "react";

import { useOnUnmount } from "@/hooks/useOnUnmount";
import type { AdminOrganization } from "@/lib/gramAdminApi";
import { formatTrialTimeRemaining } from "@/lib/trialDates";
import { TRIAL_LABELS } from "@/lib/trialLabels";

const MINUTE_MS = 60_000;
const HOUR_MS = 60 * MINUTE_MS;
const DAY_MS = 24 * HOUR_MS;
const MAX_TIMEOUT_MS = 2_147_483_647;

function stateLabel(state: unknown): string {
  switch (state) {
    case "running":
    case "ending_soon":
      return "Live";
    case "converted":
      return "Converted";
    case "demoted":
      return "Demoted";
    case "expired":
      return "Expired";
    case undefined:
    case null:
      return "No trial";
    default:
      return "Unknown";
  }
}

function nextRemainingChange(remaining: number): number {
  if (remaining > 72 * HOUR_MS) {
    return Math.max(72 * HOUR_MS, (Math.ceil(remaining / DAY_MS) - 1) * DAY_MS);
  }
  if (remaining > 24 * HOUR_MS) {
    return (Math.ceil(remaining / HOUR_MS) - 1) * HOUR_MS;
  }
  return (Math.ceil(remaining / MINUTE_MS) - 1) * MINUTE_MS;
}

function tierLabel(tier: string | undefined): string {
  return tier === "enterprise" ? "Enterprise" : "Unknown";
}

function dateLabel(iso: string | undefined): string {
  if (!iso) return "Unknown";
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? "Unknown" : date.toLocaleDateString();
}

function Fact({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="grid grid-cols-1 gap-1 py-1 sm:grid-cols-[7.5rem_minmax(0,1fr)] sm:items-center sm:gap-3">
      <span data-slot="field-label" className="text-muted-foreground text-sm">
        {label}
      </span>
      <span className="text-sm">{children}</span>
    </div>
  );
}

// Exact trial fields belong in Details. The side card summarizes the same trial
// for action, rather than replacing these record facts with another table.
export function TrialFacts({ org }: { org: AdminOrganization }): JSX.Element {
  const live =
    org.trial_state === "running" || org.trial_state === "ending_soon";
  const [now, setNow] = useState(() => new Date());
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const endTime = new Date(org.trial_ends_at ?? "").getTime();
  const attachExpiration = useCallback(
    (node: HTMLDivElement | null) => {
      clearTimeout(timer.current);
      timer.current = undefined;
      if (!node || !live || Number.isNaN(endTime)) return;

      const tick = () => {
        const currentTime = Date.now();
        const remaining = endTime - currentTime;
        if (remaining <= 0) {
          setNow(new Date(currentTime));
          return;
        }
        timer.current = setTimeout(tick, Math.min(remaining, MAX_TIMEOUT_MS));
      };

      tick();
    },
    [endTime, live],
  );
  useOnUnmount(() => clearTimeout(timer.current));
  const expired = live && !Number.isNaN(endTime) && endTime <= now.getTime();
  const lifecycleFact =
    org.trial_state === "converted"
      ? { label: "Conversion date", date: org.trial_converted_at }
      : org.trial_state === "demoted"
        ? { label: "Demotion date", date: org.trial_demoted_at }
        : undefined;
  const state =
    org.trial_state === "none" || !org.trial_state
      ? "No trial"
      : expired
        ? TRIAL_LABELS.expired
        : (TRIAL_LABELS[org.trial_state] ?? stateLabel(org.trial_state));

  return (
    <div ref={attachExpiration} className="mt-5 border-t pt-5">
      <Fact label="Trial state">{state}</Fact>
      {state !== "No trial" && (
        <>
          <Fact label="Trial tier">{tierLabel(org.trial_tier)}</Fact>
          <Fact label="End date">{dateLabel(org.trial_ends_at)}</Fact>
          {lifecycleFact && (
            <Fact label={lifecycleFact.label}>
              {dateLabel(lifecycleFact.date)}
            </Fact>
          )}
        </>
      )}
    </div>
  );
}

export function TrialSummary({ org }: { org: AdminOrganization }): JSX.Element {
  const live =
    org.trial_state === "running" || org.trial_state === "ending_soon";
  const [now, setNow] = useState(() => new Date());
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const endTime = new Date(org.trial_ends_at ?? "").getTime();

  const attachRemainingTime = useCallback(
    (node: HTMLParagraphElement | null) => {
      clearTimeout(timer.current);
      timer.current = undefined;
      if (!node) return;

      const tick = () => {
        const currentTime = Date.now();
        setNow(new Date(currentTime));
        const remaining = endTime - currentTime;
        if (Number.isNaN(endTime) || remaining <= 0) return;
        const delay = remaining - nextRemainingChange(remaining);
        timer.current = setTimeout(tick, Math.min(delay, MAX_TIMEOUT_MS));
      };

      tick();
    },
    [endTime],
  );

  useOnUnmount(() => clearTimeout(timer.current));

  const expired = live && !Number.isNaN(endTime) && endTime <= now.getTime();
  const remaining =
    live && !expired
      ? formatTrialTimeRemaining(org.trial_ends_at, now)
      : undefined;
  const hero = remaining
    ? `${remaining} left`
    : expired || org.trial_state === "expired"
      ? "Trial ended"
      : org.trial_state === "demoted"
        ? "Trial demoted"
        : "Trial status unknown";
  const status = expired
    ? TRIAL_LABELS.expired
    : org.trial_state
      ? (TRIAL_LABELS[org.trial_state] ?? stateLabel(org.trial_state))
      : stateLabel(org.trial_state);

  return (
    <div>
      <div className="flex items-center justify-between gap-3">
        <h5 className="text-sm font-semibold">
          {live && !expired ? "Active trial" : "Enterprise trial"}
        </h5>
        <span className="text-muted-foreground rounded-full border px-2 py-0.5 text-[0.6875rem] font-medium tracking-wide">
          {status.toUpperCase()}
        </span>
      </div>
      <p
        ref={live && !expired ? attachRemainingTime : undefined}
        className="mt-5 text-2xl font-semibold tracking-tight"
      >
        {hero}
      </p>
      <p className="text-muted-foreground mt-1 text-xs">
        End date {dateLabel(org.trial_ends_at)} · {tierLabel(org.trial_tier)}{" "}
        tier
      </p>
    </div>
  );
}
