/**
 * How long a tool call with no result reads as still running. A call with no
 * result after this long most likely ended without reporting one (an agent
 * plugin that sends no after-call event for a failed tool, for example), so it
 * reads as unknown rather than pending. It never reads as a success.
 */
export const STALE_PENDING_TOOL_CALL_MS = 10 * 60 * 1000;

/** Whether a call that started at startedAtMs and has no result yet is old
 * enough that its outcome is unknown rather than pending. */
export function isStalePendingToolCall(
  startedAtMs: number,
  nowMs: number = Date.now(),
): boolean {
  return nowMs - startedAtMs >= STALE_PENDING_TOOL_CALL_MS;
}
