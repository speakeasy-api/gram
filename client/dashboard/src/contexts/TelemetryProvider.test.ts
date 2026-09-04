import { describe, expect, it } from "vitest";
import { shouldUseDevTelemetry } from "./TelemetryProvider";

describe("shouldUseDevTelemetry", () => {
  it("enables every flag on localhost", () => {
    expect(shouldUseDevTelemetry("https://localhost:8080")).toBe(true);
    expect(shouldUseDevTelemetry("http://localhost:5173")).toBe(true);
  });

  it("enables every flag on PR preview hosts", () => {
    expect(shouldUseDevTelemetry("https://pr-6012.dev.getgram.ai")).toBe(true);
    expect(shouldUseDevTelemetry("https://pr-1.dev.getgram.ai")).toBe(true);
  });

  it("keeps shared staging and production on real PostHog", () => {
    expect(shouldUseDevTelemetry("https://dev.getgram.ai")).toBe(false);
    expect(shouldUseDevTelemetry("https://app.getgram.ai")).toBe(false);
    expect(shouldUseDevTelemetry("https://staging.getgram.ai")).toBe(false);
  });
});
