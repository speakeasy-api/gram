import { useCallback, useRef, useState, type JSX } from "react";

import { Trial } from "@/components/Trial";
import { useOnUnmount } from "@/hooks/useOnUnmount";
import type { AdminOrganization } from "@/lib/gramAdminApi";
import { formatTrialTimeRemaining } from "@/lib/trialDates";
import { TRIAL_LABELS } from "@/lib/trialLabels";

const MINUTE_MS = 60_000;
const HOUR_MS = 60 * MINUTE_MS;
const DAY_MS = 24 * HOUR_MS;
const MAX_TIMEOUT_MS = 2_147_483_647;

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
    <div className="grid grid-cols-[7rem_1fr] gap-3">
      <span className="text-muted-foreground">{label}</span>
      <span>{children}</span>
    </div>
  );
}

// Detail-only trial facts. List and peek surfaces deliberately keep using the
// compact Trial component; this view has room for exact and changing facts.
export function TrialFacts({ org }: { org: AdminOrganization }): JSX.Element {
  const live =
    org.trial_state === "running" || org.trial_state === "ending_soon";
  const [now, setNow] = useState(() => new Date());
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const end = new Date(org.trial_ends_at ?? "");
  const endTime = end.getTime();

  const attachRemainingTime = useCallback(
    (node: HTMLSpanElement | null) => {
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

      // The component may have spent time in a completed state with no timer.
      // Refresh before scheduling whenever the countdown is attached.
      tick();
    },
    [endTime],
  );

  useOnUnmount(() => clearTimeout(timer.current));

  if (org.trial_state === "none" || !org.trial_state) {
    return <span className="text-muted-foreground text-sm">No trial</span>;
  }

  const expired = live && !Number.isNaN(endTime) && endTime <= now.getTime();

  // Completed states retain their existing compact presentation. Their stored
  // conversion and demotion dates are intentionally not presented yet.
  if (!live) return <Trial org={org} />;

  // A live server state can become locally elapsed before the next refresh.
  // Keep its presentation consistent and detach the countdown immediately.
  if (expired) return <Trial org={{ ...org, trial_state: "expired" }} />;

  const remaining = formatTrialTimeRemaining(org.trial_ends_at, now);

  return (
    <div className="space-y-1 text-sm">
      <Fact label="State">{TRIAL_LABELS[org.trial_state] ?? "Unknown"}</Fact>
      <Fact label="Tier">{tierLabel(org.trial_tier)}</Fact>
      <Fact label="Ends">{dateLabel(org.trial_ends_at)}</Fact>
      <Fact label="Remaining">
        <span ref={attachRemainingTime}>{remaining ?? "Unknown"}</span>
      </Fact>
    </div>
  );
}
