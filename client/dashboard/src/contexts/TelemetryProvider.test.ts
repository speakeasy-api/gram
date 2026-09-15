import { describe, expect, it, vi } from "vitest";
import { nullTelemetry, type Telemetry } from "./Telemetry";
import {
  failOpenMissingFlags,
  shouldFailOpenMissingFlags,
  shouldUseDevTelemetry,
} from "./TelemetryProvider";

describe("shouldUseDevTelemetry", () => {
  it("enables every flag on localhost only", () => {
    expect(shouldUseDevTelemetry("https://localhost:8080")).toBe(true);
    expect(shouldUseDevTelemetry("http://localhost:5173")).toBe(true);
  });

  it("does not force every flag on outside localhost", () => {
    expect(shouldUseDevTelemetry("https://pr-6012.dev.getgram.ai")).toBe(false);
    expect(shouldUseDevTelemetry("https://dev.getgram.ai")).toBe(false);
    expect(shouldUseDevTelemetry("https://app.getgram.ai")).toBe(false);
    expect(shouldUseDevTelemetry("https://staging.getgram.ai")).toBe(false);
  });
});

describe("shouldFailOpenMissingFlags", () => {
  it("fail-opens missing flags on PR preview hosts", () => {
    expect(shouldFailOpenMissingFlags("https://pr-6012.dev.getgram.ai")).toBe(
      true,
    );
    expect(shouldFailOpenMissingFlags("https://pr-1.dev.getgram.ai")).toBe(
      true,
    );
  });

  it("does not fail-open on localhost, staging, or production", () => {
    expect(shouldFailOpenMissingFlags("https://localhost:8080")).toBe(false);
    expect(shouldFailOpenMissingFlags("https://dev.getgram.ai")).toBe(false);
    expect(shouldFailOpenMissingFlags("https://app.getgram.ai")).toBe(false);
    expect(shouldFailOpenMissingFlags("https://staging.getgram.ai")).toBe(
      false,
    );
  });
});

describe("failOpenMissingFlags", () => {
  function wrap(value: boolean | undefined): Telemetry {
    return failOpenMissingFlags({
      ...nullTelemetry,
      isFeatureEnabled: vi.fn(() => value) as Telemetry["isFeatureEnabled"],
    });
  }

  it("treats a missing flag as enabled", () => {
    expect(wrap(undefined).isFeatureEnabled("gram-risk-watchdog")).toBe(true);
  });

  it("preserves an explicit off so rollouts can be tested", () => {
    expect(wrap(false).isFeatureEnabled("gram-risk-watchdog")).toBe(false);
  });

  it("preserves an explicit on", () => {
    expect(wrap(true).isFeatureEnabled("gram-risk-watchdog")).toBe(true);
  });

  it("keeps PostHog methods bound to the original instance", () => {
    class Stub {
      label = "source";
      isFeatureEnabled() {
        return undefined;
      }
      onFeatureFlags() {
        if (this.label !== "source") {
          throw new Error("onFeatureFlags lost its receiver");
        }
        return () => {};
      }
      capture() {
        return { uuid: "", event: "", properties: {} };
      }
      identify() {
        if (this.label !== "source") {
          throw new Error("identify lost its receiver");
        }
      }
      register() {}
      reset() {}
      group() {}
    }

    const source = new Stub() as unknown as Telemetry;
    const wrapped = failOpenMissingFlags(source);

    expect(wrapped.onFeatureFlags).toBeTypeOf("function");
    wrapped.onFeatureFlags(() => {});
    wrapped.identify("user@example.com", {});
  });
});
