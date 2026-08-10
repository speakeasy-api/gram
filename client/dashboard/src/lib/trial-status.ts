export const MILLISECONDS_PER_DAY = 24 * 60 * 60 * 1000;

export type TrialStatus = {
  dayNumber: number;
  totalDays: number;
  remainingDays: number;
  progress: number;
};

export function getTrialStatus(
  trial: { startedAt: string; endsAt: string } | null | undefined,
  now: Date,
): TrialStatus | null {
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
  if (duration <= 0 || nowTime >= endsAt) {
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
