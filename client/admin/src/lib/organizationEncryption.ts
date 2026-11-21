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
