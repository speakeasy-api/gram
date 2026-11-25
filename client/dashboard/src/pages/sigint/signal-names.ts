import type { SigintSignal } from "@gram/client/models/components/sigintsignal.js";

export function duplicateNameCounts(
  signals: SigintSignal[],
): Map<string, number> {
  const counts = new Map<string, number>();
  for (const signal of signals) {
    counts.set(signal.name, (counts.get(signal.name) ?? 0) + 1);
  }
  return counts;
}
