import { getHttpStatusCode } from "@/lib/route-errors";

/**
 * Shown when a tool-list save is refused because the toolset changed after it
 * was loaded. The save sends the version_token it read, so a 409 here means
 * someone else's edit landed first and ours was not applied.
 */
export const TOOLSET_CHANGED_MESSAGE =
  "Someone else changed this toolset after you opened it, so your change was not saved. Reload to see the latest tools, then try again.";

/** True when a toolsets.update was refused because its version_token is stale. */
export function isToolsetVersionConflict(error: unknown): boolean {
  return getHttpStatusCode(error) === 409;
}

/** Readable toast copy for a failed tool-list save. */
export function toolsetSaveErrorMessage(
  error: unknown,
  fallback: string,
): string {
  if (isToolsetVersionConflict(error)) return TOOLSET_CHANGED_MESSAGE;
  if (error instanceof Error && error.message.trim() !== "") {
    return error.message;
  }
  return fallback;
}
