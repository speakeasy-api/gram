export type TokenizerRange = { min: number; max: number };

export type Estimate = { low: number; high: number };

export type EstimateResult =
  | { ok: true; value: Estimate }
  | { ok: false; error: string };

export const HIDDEN_MULTIPLIERS = {
  lowEstimate: 3,
  highEstimate: 2,
} as const;

const TOKEN_COUNT_ERROR =
  "Enter a whole monthly token count from 0 to 9,007,199,254,740,991.";
const TOKENIZER_RANGE_ERROR =
  "Enter tokenizer ratios greater than 0, with minimum no greater than maximum.";

export function estimateSTokens(
  providerTokens: number,
  providerTokensPerSToken: TokenizerRange,
): EstimateResult {
  if (
    !Number.isFinite(providerTokens) ||
    !Number.isSafeInteger(providerTokens) ||
    providerTokens < 0
  ) {
    return { ok: false, error: TOKEN_COUNT_ERROR };
  }

  const { min, max } = providerTokensPerSToken;
  if (
    !Number.isFinite(min) ||
    !Number.isFinite(max) ||
    min <= 0 ||
    max <= 0 ||
    min > max
  ) {
    return { ok: false, error: TOKENIZER_RANGE_ERROR };
  }

  const low = providerTokens / HIDDEN_MULTIPLIERS.lowEstimate / max;
  const high = providerTokens / HIDDEN_MULTIPLIERS.highEstimate / min;
  // A ratio small enough to overflow the division is a ratio, not a range.
  if (!Number.isFinite(low) || !Number.isFinite(high)) {
    return { ok: false, error: TOKENIZER_RANGE_ERROR };
  }

  return { ok: true, value: { low, high } };
}

export function sumEstimates(estimates: Estimate[]): Estimate {
  let low = 0;
  let high = 0;

  for (const estimate of estimates) {
    low += estimate.low;
    high += estimate.high;
  }

  return { low, high };
}
