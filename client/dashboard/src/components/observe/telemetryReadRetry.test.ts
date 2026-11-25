import { describe, expect, it } from "vitest";
import { telemetryReadRetry } from "./telemetryReadRetry";

function httpError(statusCode: number): Error {
  return Object.assign(new Error("request failed"), { statusCode });
}

describe("telemetryReadRetry", () => {
  it("never retries a read the server answered with an error", () => {
    expect(telemetryReadRetry(0, httpError(500))).toBe(false);
    expect(telemetryReadRetry(0, httpError(503))).toBe(false);
    expect(telemetryReadRetry(0, httpError(400))).toBe(false);
  });

  it("retries a read that got no answer once", () => {
    const dropped = new TypeError("Failed to fetch");
    expect(telemetryReadRetry(0, dropped)).toBe(true);
    expect(telemetryReadRetry(1, dropped)).toBe(false);
  });

  it("retries the SDK's connection error once", () => {
    const dropped = Object.assign(new Error("Unable to make request"), {
      name: "ConnectionError",
    });
    expect(telemetryReadRetry(0, dropped)).toBe(true);
    expect(telemetryReadRetry(1, dropped)).toBe(false);
  });

  it("never retries an error that would fail the same way again", () => {
    const validation = Object.assign(new Error("Response validation failed"), {
      name: "ResponseValidationError",
    });
    const aborted = new DOMException(
      "The operation was aborted.",
      "AbortError",
    );
    expect(telemetryReadRetry(0, validation)).toBe(false);
    expect(telemetryReadRetry(0, aborted)).toBe(false);
    expect(telemetryReadRetry(0, new TypeError("x is not a function"))).toBe(
      false,
    );
  });
});
