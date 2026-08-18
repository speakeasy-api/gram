import type {
  InferenceSpendCap,
  InferenceSpendCapKeyType,
} from "@gram/client/models/components/inferencespendcap.js";

type CapCopy = {
  /** The functional name of the cap, as a sentence-leading noun phrase. */
  name: string;
  /** The identifier-free half of the cap's element ids. */
  slug: string;
};

/**
 * Everything the dashboard says about a Gram-managed inference key, keyed by
 * the API's own key type.
 *
 * One record rather than a mapping function per string: the label and the
 * element ids have to move together when a cap is renamed, and the API's
 * identifiers must not reach a customer's screen.
 */
const CAP_COPY: Record<InferenceSpendCapKeyType, CapCopy> = {
  internal: { name: "Security inference", slug: "security" },
  chat: { name: "Other inference", slug: "other" },
};

/**
 * The display order the caps are always rendered in.
 *
 * Invoiced inference first: it is the one an organization is paying for, so it
 * leads wherever both appear.
 */
const CAP_ORDER: readonly InferenceSpendCapKeyType[] = ["chat", "internal"];

/** The label for a cap. The one place a key type becomes words. */
export function inferenceCapLabel(keyType: InferenceSpendCapKeyType): string {
  return `${CAP_COPY[keyType].name} cap`;
}

/** The id of a cap's own amount field, unique per key and free of identifiers. */
export function inferenceCapFieldId(keyType: InferenceSpendCapKeyType): string {
  return `inference-cap-${CAP_COPY[keyType].slug}-amount`;
}

/**
 * The highest cap threshold this month's spend has crossed.
 *
 * The same ladder the threshold emails walk, read the same way — the percentage
 * is truncated before it is compared — so a meter can't show a band the
 * customer hasn't been emailed about, or stay quiet through one they have. 0
 * means the spend is below every threshold.
 */
export type SpendCapThreshold = 0 | 50 | 75 | 90 | 100;

export function crossedSpendCapThreshold(
  used: number,
  cap: number,
): SpendCapThreshold {
  if (!Number.isFinite(used) || !Number.isFinite(cap) || cap <= 0) return 0;

  const percent = Math.floor((used / cap) * 100);
  if (percent < 50) return 0;
  if (percent < 75) return 50;
  if (percent < 90) return 75;
  if (percent < 100) return 90;
  return 100;
}

/** How much of the cap meter is filled, clamped so overage can't overflow it. */
export function spendCapFillPercent(used: number, cap: number): number {
  if (!Number.isFinite(used) || !Number.isFinite(cap) || cap <= 0) return 0;
  return Math.min(100, Math.max(0, (used / cap) * 100));
}

/**
 * The caps in display order.
 *
 * The endpoint returns whatever this organization has materialized, in
 * whatever order the rows come back. Sorting here is what stops a refetch from
 * reordering the controls under someone mid-edit.
 */
export function sortInferenceCaps(
  caps: readonly InferenceSpendCap[],
): InferenceSpendCap[] {
  return [...caps].sort(
    (a, b) => CAP_ORDER.indexOf(a.keyType) - CAP_ORDER.indexOf(b.keyType),
  );
}
