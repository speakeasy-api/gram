import { crossedSpendCapThreshold } from "@/components/billing/payg-billing-estimate";
import type {
  InferenceSpendCap,
  InferenceSpendCapKeyType,
} from "@gram/client/models/components/inferencespendcap.js";

/**
 * The anchor the inference cap section carries on the billing page, so a banner
 * reporting a cap that has been reached can link straight at the controls that
 * end the pause.
 *
 * Each control carries its own anchor derived from this one, so a banner lands
 * on the single cap it is about rather than the top of the section. The section
 * anchor stays a real element of its own: it is where a link lands when the cap
 * it names isn't on screen — during a trial, or before the list has loaded.
 */
export const INFERENCE_CAPS_ANCHOR = "inference-caps";

type CapCopy = {
  /** The functional name of the cap, as a sentence-leading noun phrase. */
  name: string;
  /** The URL-facing half of the anchor. Never an API identifier. */
  slug: string;
  /** What stops when this cap is reached, and how to start it again. */
  paused: string;
  /**
   * How the cap's own month relates to the billing cycle. Only true of a key
   * that has a cap set, so it is kept apart from `invoice` — a key with no cap
   * has no month to reset.
   */
  capReset: string;
  /**
   * How this key's spend relates to the pay-as-you-go invoice. True whether or
   * not a cap is set: an uncapped key still spends money, and this is what says
   * whether that money reaches the invoice.
   */
  invoice: string;
};

/**
 * Everything the dashboard says about a Gram-managed inference key, keyed by
 * the API's own key type.
 *
 * One record rather than a mapping function per string: the label, the anchor
 * and the copy all have to move together when a cap is renamed, and the API's
 * identifiers must not reach a customer's screen or their address bar.
 */
const CAP_COPY: Record<InferenceSpendCapKeyType, CapCopy> = {
  internal: {
    name: "Security inference",
    slug: "security",
    paused:
      "The automated analysis Gram runs over this organization's traffic, including security scanning, is paused for the rest of the month. Raise the cap to start it again.",
    capReset: "This monthly cap resets on the first of the month.",
    invoice:
      "Gram funds this inference, so it never reaches your invoice and isn't part of the estimate above.",
  },
  chat: {
    name: "Other inference",
    slug: "other",
    paused:
      "Assistants and the other AI-powered dashboard experiences are paused for the rest of the month. Raise the cap to start them again.",
    capReset:
      "This monthly cap resets on the first of the month, so it doesn't line up with the billing cycle above and isn't the figure in the estimate.",
    invoice:
      "This inference is billed to this organization as its own line on the invoice.",
  },
};

/**
 * The display order the caps are always rendered in.
 *
 * Invoiced inference first: it is the one an organization is paying for, so it
 * leads wherever both appear — the section, the usage meters, and the banners.
 */
const CAP_ORDER: readonly InferenceSpendCapKeyType[] = ["chat", "internal"];

/** The label for a cap. The one place a key type becomes words. */
export function inferenceCapLabel(keyType: InferenceSpendCapKeyType): string {
  return `${CAP_COPY[keyType].name} cap`;
}

/** The anchor for a cap's own control, unique per key and free of identifiers. */
export function inferenceCapAnchor(keyType: InferenceSpendCapKeyType): string {
  return `${INFERENCE_CAPS_ANCHOR}-${CAP_COPY[keyType].slug}`;
}

/** Whether an element id is one this section answers for. */
export function isInferenceCapAnchor(id: string): boolean {
  if (id === INFERENCE_CAPS_ANCHOR) return true;
  return CAP_ORDER.some((keyType) => inferenceCapAnchor(keyType) === id);
}

/** What has stopped, for the banner that reports a cap has been reached. */
export function inferenceCapPausedNote(
  keyType: InferenceSpendCapKeyType,
): string {
  return CAP_COPY[keyType].paused;
}

/**
 * How this cap's monthly spend relates to the pay-as-you-go invoice, for a key
 * that has a cap set: when the cap's month rolls over, and where the spend
 * lands.
 */
export function inferenceCapBillingNote(
  keyType: InferenceSpendCapKeyType,
): string {
  return `${CAP_COPY[keyType].capReset} ${CAP_COPY[keyType].invoice}`;
}

/**
 * The invoice half alone, for a key with no cap set.
 *
 * The cap-reset sentence would contradict the "No cap is set" line it renders
 * directly under, but where the spend lands is still worth saying — it is the
 * only thing that tells the invoiced key from the one Gram funds.
 */
export function inferenceCapInvoiceNote(
  keyType: InferenceSpendCapKeyType,
): string {
  return CAP_COPY[keyType].invoice;
}

/**
 * The banner's call to action.
 *
 * Both caps can be reached in the same month, so the two banners stack — and
 * two buttons reading "Raise cap" would leave a screen reader user choosing
 * between identical names.
 */
export function inferenceCapRaiseLabel(
  keyType: InferenceSpendCapKeyType,
): string {
  return `Raise ${CAP_COPY[keyType].name.toLowerCase()} cap`;
}

/**
 * Whether this month's spend has reached the cap.
 *
 * A cap of zero is "no cap configured", not "nothing may be spent" — the
 * endpoint reports it for keys that never had one set, and treating it as
 * reached would pause every one of them.
 *
 * The comparison is inclusive because inference stops *at* the cap rather than
 * past it, so the boundary itself is already the paused state.
 */
export function isInferenceCapReached(
  cap: { monthlyCredits: number; creditsUsed: number } | undefined,
): boolean {
  return (
    cap !== undefined &&
    crossedSpendCapThreshold(cap.creditsUsed, cap.monthlyCredits) === 100
  );
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
