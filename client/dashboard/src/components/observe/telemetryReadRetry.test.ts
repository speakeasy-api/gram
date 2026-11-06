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
});
