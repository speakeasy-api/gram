const SECONDS_PER_HOUR = 3_600;
const SECONDS_PER_DAY = 86_400;

/**
 * A duration of a day or more in days and hours, such as "3d 20h", or null
 * for anything shorter so the caller keeps its own format. Minutes alone
 * stop reading at this length: "5570m 34s" is close to four days, but the
 * reader has to work that out.
 */
export function formatDaysHours(totalSeconds: number): string | null {
  if (!Number.isFinite(totalSeconds)) return null;
  if (totalSeconds < SECONDS_PER_DAY) return null;
  const days = Math.floor(totalSeconds / SECONDS_PER_DAY);
  const hours = Math.floor((totalSeconds % SECONDS_PER_DAY) / SECONDS_PER_HOUR);
  if (hours === 0) return `${days}d`;
  return `${days}d ${hours}h`;
}
