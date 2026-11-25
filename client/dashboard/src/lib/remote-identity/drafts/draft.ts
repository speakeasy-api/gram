/** One thing wrong with a draft, optionally pointing at the field. */
export type DraftError = {
  readonly path?: string;
  readonly message: string;
};

/**
 * Server state you are editing.
 *
 * isDirty is a comparison against the baseline, never a flag meaning "the
 * operator touched something". That distinction is the whole point of the
 * type: the interaction-based version shipped here twice, and both times it
 * offered to save a no-op.
 *
 * isValid is separate because Save needs both answers — is there anything to
 * do, and can it be done — and they are not the same question.
 */
export type Draft<T> = {
  readonly baseline: T;
  readonly values: T;
  readonly isDirty: boolean;
  readonly isValid: boolean;
  /** The baseline moved while this draft was dirty. */
  readonly conflict: boolean;
  readonly errors: readonly DraftError[];
};

export function seedDraft<T>(
  baseline: T,
  validate?: (values: T) => readonly DraftError[],
): Draft<T> {
  const errors = validate?.(baseline) ?? [];
  return {
    baseline,
    values: baseline,
    isDirty: false,
    isValid: errors.length === 0,
    conflict: false,
    errors,
  };
}

/**
 * Server state moved while the operator was editing — a refetch noticed
 * someone else's change, or our own write landed.
 *
 * Nothing to lose means take the fresher state. Otherwise keep what they
 * typed, but move the baseline anyway: a dirty draft measured against a stale
 * snapshot plans writes for a world that is already gone. Then say so, so the
 * surface can tell them rather than silently diverging.
 */
export function reconcileDraft<T>(
  draft: Draft<T>,
  incoming: T,
  validate?: (values: T) => readonly DraftError[],
): Draft<T> {
  if (!draft.isDirty) return seedDraft(incoming, validate);
  // A refetch that returns what we already had is not someone else's edit.
  // Reporting it as a conflict would send the operator through a reload for
  // nothing, every time a poll or an invalidation happened to land mid-edit.
  if (sameBaseline(draft.baseline, incoming)) return draft;
  return { ...draft, baseline: incoming, conflict: true };
}

/**
 * Baselines here are plain server records — no cycles, no class instances, no
 * undefined-vs-missing distinction that matters — so a structural compare is
 * both sufficient and cheaper to reason about than a hand-written one per T.
 */
function sameBaseline<T>(a: T, b: T): boolean {
  if (Object.is(a, b)) return true;
  try {
    return JSON.stringify(a) === JSON.stringify(b);
  } catch {
    return false;
  }
}
