import { ASSISTANT_SETUP_THREAD_STORAGE_PREFIX } from "@/lib/local-storage-keys";

// Remembers which chat backs an assistant's onboarding "setup" thread, so
// reopening the assistant's setup restores the prior conversation instead of
// starting a fresh one. The setup chat is a plain project chat created through
// the completions proxy — it isn't linked to the assistant server-side, so
// `chat.list?assistant_id` never returns it and there's no server-side lookup.
// We persist the (project, assistant) → chat id mapping client-side instead.
//
// All access is wrapped in try/catch: localStorage throws in private-mode
// Safari and when the quota is exceeded, and losing the mapping only degrades
// to the pre-existing "starts a new thread" behaviour, never breaks the page.

function storageKey(projectId: string, assistantId: string): string {
  return `${ASSISTANT_SETUP_THREAD_STORAGE_PREFIX}${projectId}:${assistantId}`;
}

export function readSetupThreadId(
  projectId: string,
  assistantId: string,
): string | null {
  if (!assistantId) return null;
  try {
    return window.localStorage.getItem(storageKey(projectId, assistantId));
  } catch {
    return null;
  }
}

export function writeSetupThreadId(
  projectId: string,
  assistantId: string,
  chatId: string,
): void {
  if (!assistantId || !chatId) return;
  try {
    window.localStorage.setItem(storageKey(projectId, assistantId), chatId);
  } catch {
    // Ignore — see module comment.
  }
}

export function clearSetupThreadId(
  projectId: string,
  assistantId: string,
): void {
  if (!assistantId) return;
  try {
    window.localStorage.removeItem(storageKey(projectId, assistantId));
  } catch {
    // Ignore — see module comment.
  }
}
