import { describe, expect, it } from "vitest";
import {
  CREDITS_EXHAUSTED_MESSAGE,
  describeStreamError,
} from "./streamErrorMessage";

describe("describeStreamError", () => {
  it("matches Gram goa 402 with name=insufficient_credits at top-level", () => {
    const error = {
      name: "insufficient_credits",
      message: "token balance exhausted",
      statusCode: 402,
    };
    expect(describeStreamError(error)).toBe(CREDITS_EXHAUSTED_MESSAGE);
  });

  it("matches Gram error packed into responseBody JSON", () => {
    const error = {
      responseBody: JSON.stringify({
        name: "insufficient_credits",
        message: "token balance exhausted",
      }),
    };
    expect(describeStreamError(error)).toBe(CREDITS_EXHAUSTED_MESSAGE);
  });

  it("matches OpenRouter-shaped 'requires more credits' message", () => {
    const error = {
      responseBody: JSON.stringify({
        error: { message: "This request requires more credits to complete" },
      }),
    };
    expect(describeStreamError(error)).toBe(CREDITS_EXHAUSTED_MESSAGE);
  });

  it("matches raw 402 status without a body fingerprint", () => {
    const error = { statusCode: 402, message: "Payment required" };
    expect(describeStreamError(error)).toBe(CREDITS_EXHAUSTED_MESSAGE);
  });

  it("matches a thrown Error whose message carries the fingerprint", () => {
    const error = new Error(
      "AI_APICallError: token balance exhausted for organization",
    );
    expect(describeStreamError(error)).toBe(CREDITS_EXHAUSTED_MESSAGE);
  });

  it("descends into nested .cause for AI SDK call-wrapped errors", () => {
    const cause = new Error("token balance exhausted");
    const wrapped = Object.assign(new Error("Stream failed"), { cause });
    expect(describeStreamError(wrapped)).toBe(CREDITS_EXHAUSTED_MESSAGE);
  });

  it("matches AI_RetryError carrying a 402 on lastError (bare-status path)", () => {
    // This is the real-world shape that produces the 'Failed to load
    // resource: status 402 ()' console line — empty body, only the status
    // survives, and the AI SDK has wrapped the APICallError in a RetryError.
    const apiError = {
      name: "AI_APICallError",
      message: "Failed to call /chat/completions",
      statusCode: 402,
      responseBody: "",
    };
    const retryError = {
      name: "AI_RetryError",
      message: "Failed after 1 attempts",
      reason: "errorNotRetryable",
      lastError: apiError,
      errors: [apiError],
    };
    expect(describeStreamError(retryError)).toBe(CREDITS_EXHAUSTED_MESSAGE);
  });

  it("matches a 402 reported via the older response.status shape", () => {
    const error = {
      name: "FetchError",
      message: "Request failed",
      response: { status: 402 },
    };
    expect(describeStreamError(error)).toBe(CREDITS_EXHAUSTED_MESSAGE);
  });

  it("returns undefined for unrelated stream errors", () => {
    expect(describeStreamError(new Error("network down"))).toBeUndefined();
    expect(describeStreamError({ statusCode: 500 })).toBeUndefined();
    expect(
      describeStreamError({ responseBody: '{"error":{"message":"oops"}}' }),
    ).toBeUndefined();
  });

  it("survives circular error references without infinite recursion", () => {
    // AI SDK normalization mostly prevents this, but a defensive cycle guard
    // means a malformed error tree can't take down the chat with a stack
    // overflow on top of whatever already failed.
    const circular: Record<string, unknown> = { name: "Boom" };
    circular.cause = circular;
    expect(describeStreamError(circular)).toBeUndefined();
  });

  it("returns undefined for nullish / non-object errors", () => {
    expect(describeStreamError(undefined)).toBeUndefined();
    expect(describeStreamError(null)).toBeUndefined();
    expect(describeStreamError(42)).toBeUndefined();
  });
});
