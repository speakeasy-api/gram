export type TokenCountParseResult =
  | { ok: true; value: number }
  | { ok: false; error: string };

const TOKEN_COUNT_INPUT_ERROR =
  "Enter a whole monthly token count from 0 to 9,007,199,254,740,991. You can use K, M, B, or T.";

const SUFFIX_EXPONENTS: Record<string, number> = {
  "": 0,
  K: 3,
  M: 6,
  B: 9,
  T: 12,
};

const TOKEN_COUNT_PATTERN =
  /^([+-]?(?:(?:\d{1,3}(?:,\d{3})+|\d+)(?:\.\d+)?|\.\d+))\s*([KMBT])?$/i;

const DENOMINATIONS = [
  { value: 1_000_000_000_000, suffix: "T" },
  { value: 1_000_000_000, suffix: "B" },
  { value: 1_000_000, suffix: "M" },
  { value: 1_000, suffix: "K" },
] as const;

const compactNumberFormatter = new Intl.NumberFormat("en-US", {
  maximumFractionDigits: 1,
});
const integerFormatter = new Intl.NumberFormat("en-US", {
  maximumFractionDigits: 0,
});

export function parseTokenCount(input: string): TokenCountParseResult {
  const trimmed = input.trim();
  if (trimmed.length === 0 || trimmed.length > 64) {
    return { ok: false, error: TOKEN_COUNT_INPUT_ERROR };
  }

  const match = TOKEN_COUNT_PATTERN.exec(trimmed);
  // The first group is not optional in the pattern, so a match always fills
  // it; the guard is for the index type rather than for a real case.
  if (!match || match[1] === undefined) {
    return { ok: false, error: TOKEN_COUNT_INPUT_ERROR };
  }

  const numericPart = match[1].replaceAll(",", "");
  if (numericPart.startsWith("-")) {
    return { ok: false, error: TOKEN_COUNT_INPUT_ERROR };
  }

  const unsignedNumericPart = numericPart.startsWith("+")
    ? numericPart.slice(1)
    : numericPart;
  const [wholePart = "", fractionPart = ""] = unsignedNumericPart.split(".");
  const digits = `${wholePart || "0"}${fractionPart}`;
  const suffix = (match[2] ?? "").toUpperCase();
  const decimalShift = (SUFFIX_EXPONENTS[suffix] ?? 0) - fractionPart.length;
  let tokenCount = BigInt(digits);

  if (decimalShift >= 0) {
    tokenCount *= 10n ** BigInt(decimalShift);
  } else {
    const divisor = 10n ** BigInt(-decimalShift);
    if (tokenCount % divisor !== 0n) {
      return { ok: false, error: TOKEN_COUNT_INPUT_ERROR };
    }
    tokenCount /= divisor;
  }

  if (tokenCount > BigInt(Number.MAX_SAFE_INTEGER)) {
    return { ok: false, error: TOKEN_COUNT_INPUT_ERROR };
  }

  return { ok: true, value: Number(tokenCount) };
}

export function formatTokenCount(tokenCount: number): string {
  const roundedTokenCount = Math.round(tokenCount);

  for (const [index, denomination] of DENOMINATIONS.entries()) {
    if (roundedTokenCount < denomination.value) continue;

    let scaled = roundedTokenCount / denomination.value;
    let suffix: string = denomination.suffix;
    const roundedScaled = Math.round(scaled * 10) / 10;

    // 999.96M rounds to 1,000.0M, which reads better as 1B.
    const largerDenomination = index > 0 ? DENOMINATIONS[index - 1] : undefined;
    if (roundedScaled >= 1_000 && largerDenomination) {
      scaled = roundedTokenCount / largerDenomination.value;
      suffix = largerDenomination.suffix;
    }

    return `${compactNumberFormatter.format(scaled)}${suffix}`;
  }

  return integerFormatter.format(roundedTokenCount);
}
