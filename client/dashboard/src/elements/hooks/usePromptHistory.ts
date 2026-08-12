import { useCallback, useMemo, useRef } from "react";

const STORAGE_PREFIX = "gram.elements.prompt-history";
const MAX_ENTRIES = 50;

/** Slot 0 is the live draft; slots 1..n are entries[slot - 1], newest first. */
const DRAFT_SLOT = 0;

export interface PromptHistory {
  /** Persist a sent prompt at the head of the history. */
  record: (text: string) => void;
  /**
   * Move one step through the history and return the text the composer should
   * show, or null when there is nothing recorded yet.
   */
  navigate: (direction: "up" | "down", draft: string) => string | null;
  /** True while the composer still shows the text this hook last recalled. */
  isShowingRecalled: (text: string) => boolean;
}

function readEntries(key: string): string[] {
  try {
    const raw = window.localStorage.getItem(key);
    if (!raw) return [];
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((entry): entry is string => typeof entry === "string");
  } catch {
    // Private-mode denials and hand-edited values both mean "no history".
    return [];
  }
}

function writeEntries(key: string, entries: string[]): void {
  try {
    window.localStorage.setItem(key, JSON.stringify(entries));
  } catch {
    // History is a convenience; a full or blocked store must not break sending.
  }
}

/**
 * Terminal-style recall for the composer: Up walks back through prompts sent
 * from this browser, Down walks forward, and both wrap around through the
 * draft slot so the empty composer is always one step past the oldest entry.
 *
 * Entries live in localStorage, scoped per project, so history survives a
 * reload and does not leak between projects on the same origin.
 */
export function usePromptHistory(projectSlug: string): PromptHistory {
  const key = `${STORAGE_PREFIX}:${projectSlug}`;

  const slotRef = useRef(DRAFT_SLOT);
  const draftRef = useRef("");
  // The text we last handed the composer. Anything else on screen means the
  // user has typed since, so the next step starts a fresh walk from the draft.
  const appliedRef = useRef<string | null>(null);
  const keyRef = useRef(key);

  const reset = useCallback(() => {
    slotRef.current = DRAFT_SLOT;
    draftRef.current = "";
    appliedRef.current = null;
  }, []);

  // A composer that outlives a project switch must not carry the old project's
  // cursor into the new project's entries.
  if (keyRef.current !== key) {
    keyRef.current = key;
    slotRef.current = DRAFT_SLOT;
    draftRef.current = "";
    appliedRef.current = null;
  }

  const record = useCallback(
    (text: string) => {
      reset();
      const trimmed = text.trim();
      if (!trimmed) return;
      const entries = readEntries(key);
      if (entries[0] === trimmed) return;
      writeEntries(key, [trimmed, ...entries].slice(0, MAX_ENTRIES));
    },
    [key, reset],
  );

  const navigate = useCallback(
    (direction: "up" | "down", draft: string): string | null => {
      const entries = readEntries(key);
      if (entries.length === 0) return null;

      // Text the user has typed or edited since the last step ends the walk it
      // came from: it becomes the new draft, so the step starts over from there
      // and the edit is waiting at the draft slot on the way back.
      if (appliedRef.current !== draft) slotRef.current = DRAFT_SLOT;
      if (slotRef.current === DRAFT_SLOT) draftRef.current = draft;

      const slots = entries.length + 1;
      const step = direction === "up" ? 1 : slots - 1;
      const slot = (slotRef.current + step) % slots;
      slotRef.current = slot;

      const text =
        slot === DRAFT_SLOT ? draftRef.current : (entries[slot - 1] ?? "");
      appliedRef.current = text;
      return text;
    },
    [key],
  );

  const isShowingRecalled = useCallback(
    (text: string) =>
      appliedRef.current !== null && appliedRef.current === text,
    [],
  );

  return useMemo(
    () => ({ record, navigate, isShowingRecalled }),
    [record, navigate, isShowingRecalled],
  );
}
