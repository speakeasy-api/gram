// A signed decimal with no exponent — the shape a Postgres numeric serializes
// to. Anything else (an exponent, a stray character, an empty string) is not
// something this can render exactly, so it is refused rather than guessed at.
const EXACT_DECIMAL = /^([+-]?)(\d+)(?:\.(\d*))?$/;

// Group exact decimal amounts from their own digits without losing precision
// through a JavaScript number.
function groupDigits(digits: string): string {
  return digits.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

/**
 * An exact decimal USD amount as US currency prose, or null when the input
 * isn't an exact decimal — callers render a dash rather than "$NaN" beside a
 * figure a customer is about to be invoiced for.
 *
 * Every step is a string operation. The amounts here are the ones the invoice
 * is built from, and a per-token unit price carries far more precision than a
 * double preserves, so nothing is parsed, summed, or rounded: trailing zeros
 * below two decimal places are dropped, the rest of the fraction is kept
 * exactly as the server sent it.
 */
export function formatExactUsd(
  amount: string | null | undefined,
): string | null {
  if (typeof amount !== "string") return null;

  const match = EXACT_DECIMAL.exec(amount.trim());
  if (match === null) return null;

  const [, sign = "", whole = "", fraction = ""] = match;
  // "$0.00" rather than "$0.0000" for a whole amount, and "$0.00000015" intact
  // for a unit price: pad up to cents, never truncate past them.
  const cents = fraction.replace(/0+$/, "").padEnd(2, "0");
  const dollars = groupDigits(whole.replace(/^0+(?=\d)/, ""));

  // A signed zero is still zero, and "-$0.00" reads as a refund that isn't one.
  const zero = /^0*$/.test(whole) && /^0*$/.test(fraction);
  return `${sign === "-" && !zero ? "-" : ""}$${dollars}.${cents}`;
}
