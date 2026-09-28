import { describe, expect, it, vi } from "vitest";
import { nullTelemetry, type Telemetry } from "./Telemetry";
import {
  failOpenMissingFlags,
  isGramHost,
  isProdHost,
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

  it("passes variants through without failing open", () => {
    const variants: Array<string | boolean | undefined> = [
      "shadow",
      true,
      undefined,
    ];
    for (const variant of variants) {
      const wrapped = failOpenMissingFlags({
        ...nullTelemetry,
        getFeatureFlag: vi.fn(() => variant),
      });
      expect(wrapped.getFeatureFlag("gram-risk-llm-analyzer")).toBe(variant);
    }
  });

  it("keeps PostHog methods bound to the original instance", () => {
    class Stub {
      label = "source";
      isFeatureEnabled() {
        return undefined;
      }
      getFeatureFlag() {
        if (this.label !== "source") {
          throw new Error("getFeatureFlag lost its receiver");
        }
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
    expect(wrapped.getFeatureFlag("gram-risk-llm-analyzer")).toBeUndefined();
  });
});

describe("isGramHost", () => {
  it.each([
    "https://getgram.ai",
    "https://app.getgram.ai",
    "https://pr-6012.dev.getgram.ai",
    "https://ai.speakeasy.com",
    "https://dev.ai.speakeasy.com",
  ])("admits %s", (url) => {
    expect(isGramHost(url)).toBe(true);
  });

  it.each([
    "https://mygetgram.ai",
    "https://fooai.speakeasy.com",
    "https://getgram.ai.evil.com",
    "https://evil.example/getgram.ai",
    "https://speakeasy.com",
  ])("rejects %s", (url) => {
    expect(isGramHost(url)).toBe(false);
  });
});

describe("isProdHost", () => {
  it("treats only the production dashboard hosts as prod", () => {
    expect(isProdHost("https://app.getgram.ai")).toBe(true);
    expect(isProdHost("https://ai.speakeasy.com")).toBe(true);
    expect(isProdHost("https://dev.ai.speakeasy.com")).toBe(false);
    expect(isProdHost("https://dev.getgram.ai")).toBe(false);
    expect(isProdHost("https://pr-6012.dev.getgram.ai")).toBe(false);
  });
});
