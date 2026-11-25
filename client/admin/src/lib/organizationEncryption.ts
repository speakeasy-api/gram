import type { AdminOrganizationEncryption } from "@gram/admin-client/models/components/adminorganizationencryption";

import { badgeTone } from "@/lib/badgeTone";

export const TRANSITION_STATES = [
  "platform_key",
  "dual_write",
  "org_kms",
] as const;

export type TransitionState = (typeof TRANSITION_STATES)[number];

export const TRANSITION_STATE_LABELS: Record<TransitionState, string> = {
  platform_key: "Platform key",
  dual_write: "Dual write",
  org_kms: "Organization key",
};

const USE_CASE_LABELS: Record<string, string> = {
  "oauth-client-secrets": "OAuth client secrets",
  "oauth-session-tokens": "OAuth session tokens",
};

/** A readable name for a use case, falling back to its identifier for use cases this build does not know. */
export function labelForUseCase(useCase: string): string {
  return USE_CASE_LABELS[useCase] ?? useCase;
}

export function isTransitionState(value: string): value is TransitionState {
  return (TRANSITION_STATES as readonly string[]).includes(value);
}

export function transitionStateLabel(state: string): string {
  return isTransitionState(state) ? TRANSITION_STATE_LABELS[state] : state;
}

/**
 * Where one use case of one organization stands, as the badge and the actions
 * read it. org_kms without a cleared time means the clear after finalize is in
 * progress or was interrupted, so platform-key copies may remain.
 */
export type TransitionPhase =
  | "platform_key"
  | "dual_write"
  | "clearing"
  | "finalized";

export function transitionPhase(
  state: string,
  platformCopiesClearedAt: Date | undefined,
): TransitionPhase {
  if (state === "org_kms") {
    return platformCopiesClearedAt ? "finalized" : "clearing";
  }
  return state === "dual_write" ? "dual_write" : "platform_key";
}

export const TRANSITION_PHASE_LABELS: Record<TransitionPhase, string> = {
  platform_key: "Platform key",
  dual_write: "Dual write",
  clearing: "Clearing",
  finalized: "Organization key",
};

export const TRANSITION_PHASE_TONES: Record<TransitionPhase, string> = {
  platform_key: badgeTone.neutral,
  dual_write: badgeTone.warning,
  clearing: badgeTone.warning,
  finalized: badgeTone.success,
};

export function pluralize(
  count: number,
  singular: string,
  plural = `${singular}s`,
): string {
  return `${count.toLocaleString()} ${count === 1 ? singular : plural}`;
}

/** A soak time in seconds, written the way staff say it: "24 hours", "2 days". */
export function formatDuration(seconds: number): string {
  const hours = Math.round(seconds / 3600);
  if (hours >= 48 && hours % 24 === 0) return pluralize(hours / 24, "day");
  if (hours >= 1) return pluralize(hours, "hour");
  return pluralize(Math.max(1, Math.round(seconds / 60)), "minute");
}

export function formatTimestamp(date: Date | undefined): string {
  if (!date || Number.isNaN(date.getTime())) return "-";
  return `${date.toLocaleString(undefined, {
    timeZone: "UTC",
    dateStyle: "medium",
    timeStyle: "short",
  })} UTC`;
}

/** How often the page refreshes while a backfill or clear may be running. */
const REFRESH_WHILE_RUNNING_MS = 10_000;

/**
 * How long the page keeps refreshing after a run is requested, before the
 * stored state shows it started. A queued run waits for a worker, so the
 * first refresh can still read the state from before it.
 */
const REFRESH_AFTER_REQUEST_MS = 3 * 60_000;

/** Whether a backfill or a clear may be running for any use case. */
function runInProgress(encryption: AdminOrganizationEncryption): boolean {
  return encryption.useCases.some(
    (useCase) =>
      transitionPhase(useCase.state, useCase.platformCopiesClearedAt) ===
        "clearing" ||
      (useCase.state === "dual_write" &&
        useCase.lastBackfillStartedAt !== undefined &&
        useCase.lastBackfillCompletedAt === undefined),
  );
}

/**
 * How often to refresh the page: while a backfill or clear may be running, and
 * for a while after one was requested, before the stored state shows it.
 */
export function encryptionRefreshInterval(
  encryption: AdminOrganizationEncryption | undefined,
  requestedAt: number | null,
  now: number,
): number | false {
  const running = encryption !== undefined && runInProgress(encryption);
  const recentlyRequested =
    requestedAt !== null && now - requestedAt < REFRESH_AFTER_REQUEST_MS;
  return running || recentlyRequested ? REFRESH_WHILE_RUNNING_MS : false;
}
