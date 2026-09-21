import { describe, expect, it } from "vitest";

import { describeApiError } from "./api-error";

describe("describeApiError", () => {
  it("maps 412 to a precondition with the API message", () => {
    const described = describeApiError({
      statusCode: 412,
      message: "submit the Okta client id before verifying",
    });
    expect(described.kind).toBe("precondition");
    expect(described.message).toBe(
      "submit the Okta client id before verifying",
    );
  });

  it("treats 409 like a precondition", () => {
    expect(describeApiError({ statusCode: 409, message: "" }).kind).toBe(
      "precondition",
    );
  });

  it("maps 429 to rate limited and keeps the API message", () => {
    const described = describeApiError({
      statusCode: 429,
      message: "rate limit exceeded",
    });
    expect(described.kind).toBe("rate_limited");
    expect(described.message).toBe("rate limit exceeded");
  });

  it("maps 400 to a bad request", () => {
    const described = describeApiError({
      statusCode: 400,
      message: "client_id must be an Okta application client id",
    });
    expect(described.kind).toBe("bad_request");
    expect(described.title).toBe("Check the value");
  });

  it("maps 403 and 503 to their own kinds", () => {
    expect(describeApiError({ statusCode: 403, message: "" }).kind).toBe(
      "forbidden",
    );
    expect(describeApiError({ statusCode: 503, message: "" }).kind).toBe(
      "unavailable",
    );
    expect(describeApiError({ statusCode: 502, message: "" }).kind).toBe(
      "unavailable",
    );
    expect(describeApiError({ statusCode: 504, message: "" }).kind).toBe(
      "unavailable",
    );
  });

  it("falls back to fixed copy when the API message is empty or generic", () => {
    expect(describeApiError({ statusCode: 429, message: "  " }).message).toBe(
      "Too many attempts. Wait a minute and try again.",
    );
    expect(
      describeApiError({
        statusCode: 412,
        message: "API error occurred: {}",
      }).message,
    ).toBe("This is not possible in the current state.");
  });

  it("treats non-HTTP errors as other", () => {
    const described = describeApiError(new Error("network down"));
    expect(described.kind).toBe("other");
    expect(described.message).toBe("network down");
    expect(describeApiError(undefined).message).toBe(
      "The request failed. Try again.",
    );
  });
});
