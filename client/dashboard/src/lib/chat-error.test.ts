import { describe, expect, it } from "vitest";
import { extractStreamError } from "./chat-error";

describe("extractStreamError", () => {
  it("extracts OpenRouter-shaped error.message from responseBody", () => {
    const event = {
      error: {
        responseBody: JSON.stringify({
          error: { message: "model requires more credits" },
        }),
      },
    };
    expect(extractStreamError(event)).toBe("model requires more credits");
  });

  it("extracts goa ServiceError top-level message from responseBody", () => {
    // Gram's /chat/completions returns this shape on 402 insufficient_credits.
    const event = {
      error: {
        responseBody: JSON.stringify({
          name: "insufficient_credits",
          id: "abc123",
          message: "token balance exhausted",
          temporary: false,
          timeout: false,
          fault: false,
        }),
      },
    };
    expect(extractStreamError(event)).toBe("token balance exhausted");
  });

  it("falls back to error.message when responseBody is absent", () => {
    expect(extractStreamError({ error: new Error("network down") })).toBe(
      "network down",
    );
  });

  it("returns undefined for null/non-object errors", () => {
    expect(extractStreamError({ error: null })).toBeUndefined();
    expect(extractStreamError({ error: "raw string" })).toBeUndefined();
  });
});
