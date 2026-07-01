import { ASSISTANT_SETUP_THREAD_STORAGE_PREFIX } from "@/lib/local-storage-keys";

// Remembers which chat thread backs an assistant's setup conversation so that
// reopening the assistant resumes the same conversation instead of starting a
// blank one. The transcript itself is persisted server-side (history.enabled);
// this is only a pointer to the most recent setup thread. Keys are scoped per
// project + user + assistant so one assistant's thread (or another user's on a
// shared machine) never restores into the wrong place.

function setupThreadStorageKey(
  projectId: string,
  userId: string,
  assistantId: string,
): string {
  return `${ASSISTANT_SETUP_THREAD_STORAGE_PREFIX}${projectId}:${userId}:${assistantId}`;
}

export function readStoredSetupThreadId(
  projectId: string,
  userId: string,
  assistantId: string,
): string | undefined {
  try {
    return (
      window.localStorage.getItem(
        setupThreadStorageKey(projectId, userId, assistantId),
      ) ?? undefined
    );
  } catch {
    // Storage unavailable (blocked, quota, SSR) — behave as if nothing was
    // stored and start a fresh thread.
    return undefined;
  }
}

export function writeStoredSetupThreadId(
  projectId: string,
  userId: string,
  assistantId: string,
  threadId: string,
): void {
  try {
    window.localStorage.setItem(
      setupThreadStorageKey(projectId, userId, assistantId),
      threadId,
    );
  } catch {
    // Best-effort: without storage the page still works, it just won't
    // restore this thread on the next visit.
  }
}
