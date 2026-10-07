import { handleAPIError, handleError } from "@/lib/errors";
import { getHttpStatusCode } from "@/lib/route-errors";

/**
 * Shown when a tool-list save is refused because the toolset changed after it
 * was loaded. The save sends the version_token it read, so a 409 here means
 * someone else's edit landed first and ours was not applied.
 */
export const TOOLSET_CHANGED_MESSAGE =
  "Someone else changed this toolset after you opened it, so your change was not saved. Reload to see the latest tools, then try again.";

/**
 * True only for the stale-version refusal. toolsets.update returns 409 for other
 * reasons too (a taken slug, a concurrent slug swap), and those keep their own
 * message; the server names the stale field in this one.
 */
export function isToolsetVersionConflict(error: unknown): boolean {
  return (
    getHttpStatusCode(error) === 409 &&
    error instanceof Error &&
    error.message.includes("expected_version_token")
  );
}

/** Surfaces a failed tool-list save; a stale-version conflict offers a reload. */
export function handleToolsetSaveError(
  error: unknown,
  reload: () => void,
): void {
  if (isToolsetVersionConflict(error)) {
    handleError(error, {
      title: "Your change was not saved",
      message: TOOLSET_CHANGED_MESSAGE,
      persist: true,
      customAction: { label: "Reload", onClick: reload },
    });
    return;
  }
  handleAPIError(error);
}
