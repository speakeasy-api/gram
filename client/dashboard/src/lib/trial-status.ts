export const MILLISECONDS_PER_DAY = 24 * 60 * 60 * 1000;

export type TrialStatus = {
  dayNumber: number;
  totalDays: number;
  remainingDays: number;
  progress: number;
};

/** Where a trial sits relative to `now`, including after it has ended. */
export type TrialLifecycle = "none" | "active" | "expired";

type ParsedTrial = {
  startedAt: number;
  endsAt: number;
  nowTime: number;
  duration: number;
};

/**
 * Normalizes a trial and reference time into timestamps, returning null when
 * either date is unparseable, `now` is invalid, or the trial has no positive
 * duration. Shared so every trial derivation rejects the same inputs.
 */
function parseTrial(
  trial: { startedAt: string; endsAt: string } | null | undefined,
  now: Date,
): ParsedTrial | null {
  if (trial === null || trial === undefined) {
    return null;
  }

  const startedAt = Date.parse(trial.startedAt);
  const endsAt = Date.parse(trial.endsAt);
  const nowTime = now.getTime();

  if (
    !Number.isFinite(startedAt) ||
    !Number.isFinite(endsAt) ||
    !Number.isFinite(nowTime)
  ) {
    return null;
  }

  const duration = endsAt - startedAt;
  if (duration <= 0) {
    return null;
  }

  return { startedAt, endsAt, nowTime, duration };
}

export function isValidDate(value: unknown): value is Date {
  return value instanceof Date && Number.isFinite(value.getTime());
}

export function getTrialStatus(
  trial: { startedAt: string; endsAt: string } | null | undefined,
  now: Date,
): TrialStatus | null {
  const parsed = parseTrial(trial, now);
  if (parsed === null) {
    return null;
  }

  const { startedAt, endsAt, nowTime, duration } = parsed;
  if (nowTime >= endsAt) {
    return null;
  }

  const effectiveNow = Math.max(nowTime, startedAt);
  const elapsed = effectiveNow - startedAt;
  const totalDays = Math.ceil(duration / MILLISECONDS_PER_DAY);

  return {
    dayNumber: Math.floor(elapsed / MILLISECONDS_PER_DAY) + 1,
    totalDays,
    remainingDays: Math.ceil((endsAt - effectiveNow) / MILLISECONDS_PER_DAY),
    progress: Math.min(1, Math.max(0, elapsed / duration)),
  };
}

/**
 * Classifies a trial so callers can distinguish an organization whose trial
 * ended from one that never trialed. Unusable input reads as "none".
 */
export function getTrialLifecycle(
  trial: { startedAt: string; endsAt: string } | null | undefined,
  now: Date,
): TrialLifecycle {
  const parsed = parseTrial(trial, now);
  if (parsed === null) {
    return "none";
  }

  return parsed.nowTime >= parsed.endsAt ? "expired" : "active";
}

function toStringTrial(
  trial: { startedAt: Date; endsAt: Date } | null | undefined,
): { startedAt: string; endsAt: string } | null {
  if (
    trial === null ||
    trial === undefined ||
    !isValidDate(trial.startedAt) ||
    !isValidDate(trial.endsAt)
  ) {
    return null;
  }

  return {
    startedAt: trial.startedAt.toISOString(),
    endsAt: trial.endsAt.toISOString(),
  };
}

/** `getTrialStatus` for the `Date`-shaped trial carried on the session. */
export function getTrialStatusFromDates(
  trial: { startedAt: Date; endsAt: Date } | null | undefined,
  now: Date,
): TrialStatus | null {
  return getTrialStatus(toStringTrial(trial), now);
}

/** `getTrialLifecycle` for the `Date`-shaped trial carried on the session. */
export function getTrialLifecycleFromDates(
  trial: { startedAt: Date; endsAt: Date } | null | undefined,
  now: Date,
): TrialLifecycle {
  return getTrialLifecycle(toStringTrial(trial), now);
}
