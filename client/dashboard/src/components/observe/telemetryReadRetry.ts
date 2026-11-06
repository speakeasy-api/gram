/**
 * Retry policy for the tool usage reads (totals, targets, series, filter
 * options, trace lists).
 *
 * These are analytics queries over a whole window. When one fails with an
 * HTTP answer it has usually spent the server's full ClickHouse budget first
 * (a 60 second max_execution_time, or a connection the overloaded database
 * never accepted), and asking again does the same: the app-wide policy of
 * three retries kept a panel on its skeleton for four minutes and sent the
 * same expensive query four times, adding to the load that made it fail. So
 * an HTTP failure is shown at once, with the panel's own Retry, and only a
 * request that never got an answer (a dropped connection) is tried again,
 * once.
 */
export function telemetryReadRetry(
  failureCount: number,
  error: Error,
): boolean {
  if (httpStatus(error) !== undefined) return false;
  return failureCount < 1;
}

function httpStatus(error: unknown): number | undefined {
  if (!error || typeof error !== "object") return undefined;
  const { status, statusCode } = error as {
    status?: unknown;
    statusCode?: unknown;
  };
  if (typeof statusCode === "number") return statusCode;
  if (typeof status === "number") return status;
  return undefined;
}
