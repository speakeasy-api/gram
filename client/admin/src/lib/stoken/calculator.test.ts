import { describe, expect, test } from "vitest";
import {
  estimateSTokens,
  sumEstimates,
  type Estimate,
  type TokenizerRange,
} from "./calculator";

const TOKEN_COUNT_ERROR =
  "Enter a whole monthly token count from 0 to 9,007,199,254,740,991.";
const TOKENIZER_RANGE_ERROR =
  "Enter tokenizer ratios greater than 0, with minimum no greater than maximum.";

function expectError(
  providerTokens: number,
  tokenizerRange: TokenizerRange,
  error: string,
): void {
  expect(estimateSTokens(providerTokens, tokenizerRange)).toEqual({
    ok: false,
    error,
  });
}

describe("estimateSTokens", () => {
  test("calculates the conservative Anthropic scenario envelope", () => {
    expect(estimateSTokens(120_000_000, { min: 1.2, max: 1.6 })).toEqual({
      ok: true,
      value: { low: 25_000_000, high: 50_000_000 },
    });
  });

  test("rejects a ratio small enough to overflow the division", () => {
    // 1e-320 is a valid positive number, but P ÷ 2 ÷ min is Infinity.
    expectError(
      Number.MAX_SAFE_INTEGER,
      { min: 1e-320, max: 1e-320 },
      TOKENIZER_RANGE_ERROR,
    );
  });

  test("accepts zero provider usage", () => {
    expect(estimateSTokens(0, { min: 1, max: 1 })).toEqual({
      ok: true,
      value: { low: 0, high: 0 },
    });
  });

  test.each([
    -1,
    0.5,
    Number.NaN,
    Number.POSITIVE_INFINITY,
    Number.MAX_SAFE_INTEGER + 1,
  ])("rejects invalid provider token count %p", (providerTokens) => {
    expectError(providerTokens, { min: 1, max: 1 }, TOKEN_COUNT_ERROR);
  });

  test.each([
    { min: 0, max: 1 },
    { min: -1, max: 1 },
    { min: 1, max: 0 },
    { min: 1, max: -1 },
    { min: Number.NaN, max: 1 },
    { min: 1, max: Number.POSITIVE_INFINITY },
    { min: 1.6, max: 1.2 },
  ])("rejects invalid tokenizer range %o", (tokenizerRange) => {
    expectError(1, tokenizerRange, TOKENIZER_RANGE_ERROR);
  });
});

describe("sumEstimates", () => {
  test("sums low and high endpoints independently", () => {
    const anthropic = estimateSTokens(120_000_000, {
      min: 1.2,
      max: 1.6,
    });
    const openAI = estimateSTokens(100_000_000, { min: 1, max: 1 });

    if (!anthropic.ok || !openAI.ok) {
      throw new Error("Expected valid estimates");
    }

    const total = sumEstimates([anthropic.value, openAI.value]);
    expect(total.low).toBeCloseTo(58_333_333.333, 3);
    expect(total.high).toBe(100_000_000);
  });

  test("returns a zero envelope for no estimates", () => {
    expect(sumEstimates([] as Estimate[])).toEqual({ low: 0, high: 0 });
  });
});
