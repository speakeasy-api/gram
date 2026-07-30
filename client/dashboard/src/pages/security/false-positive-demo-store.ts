// TEMPORARY UX-DEMO SCAFFOLDING for AIS-321.
//
// Simulates "mark false positive" / undo entirely client-side (in-memory, resets on
// refresh) so the UX can be reviewed in a draft PR before the backend exists. Once
// risk.markFalsePositive / risk.unmarkFalsePositive / risk.listDismissedResults land,
// every call site using this store gets replaced with the generated SDK hooks and this
// file is deleted.

import { useSyncExternalStore } from "react";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";

export type DismissedFinding = {
  result: RiskResult;
  dismissedAt: Date;
};

const dismissed = new Map<string, DismissedFinding>();
const listeners = new Set<() => void>();

// useSyncExternalStore requires getSnapshot to return a referentially stable
// value between calls when nothing changed — returning a freshly-constructed
// Set/array on every call (even with identical contents) makes React think the
// store changed on every render and loops forever ("Maximum update depth
// exceeded"). These caches are rebuilt only inside emit(), once per mutation.
let idsSnapshot = new Set<string>();
let findingsSnapshot: DismissedFinding[] = [];

function emit() {
  idsSnapshot = new Set(dismissed.keys());
  findingsSnapshot = [...dismissed.values()].sort(
    (a, b) => b.dismissedAt.getTime() - a.dismissedAt.getTime(),
  );
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function dismissFindings(results: RiskResult[]): void {
  const now = new Date();
  for (const result of results) {
    dismissed.set(result.id, { result, dismissedAt: now });
  }
  emit();
}

export function undoDismiss(resultId: string): void {
  dismissed.delete(resultId);
  emit();
}

export function isDismissed(resultId: string): boolean {
  return dismissed.has(resultId);
}

/** Live set of dismissed result ids, for filtering a results list down to "not
 * dismissed" without re-fetching. */
export function useDismissedIds(): Set<string> {
  return useSyncExternalStore(
    subscribe,
    () => idsSnapshot,
    () => idsSnapshot,
  );
}

/** Live list of dismissed findings (newest first), for the Dismissed tab. */
export function useDismissedFindings(): DismissedFinding[] {
  return useSyncExternalStore(
    subscribe,
    () => findingsSnapshot,
    () => findingsSnapshot,
  );
}
