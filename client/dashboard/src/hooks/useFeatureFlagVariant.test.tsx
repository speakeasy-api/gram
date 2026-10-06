import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import {
  nullTelemetry,
  TelemetryStateProvider,
  type Telemetry,
} from "@/contexts/Telemetry";
import { useFeatureFlagVariant } from "./useFeatureFlagVariant";

type FeatureFlagsCallback = Parameters<Telemetry["onFeatureFlags"]>[0];
type FlagValue = string | boolean | undefined;

function renderFeatureFlagVariant({
  initiallyAvailable = false,
  initialValue,
}: {
  initiallyAvailable?: boolean;
  initialValue?: FlagValue;
} = {}) {
  let flagValue: FlagValue = initialValue;
  let featureFlagsCallback: FeatureFlagsCallback | undefined;
  const unsubscribe = vi.fn();
  // posthog-js returns the variant key for a multivariate flag and a boolean
  // for a boolean flag from getFeatureFlag, while isFeatureEnabled is true
  // for any variant (including one named "off").
  const getFeatureFlag = vi.fn((_flag: string) => flagValue);
  const isFeatureEnabled = vi.fn((_flag: string) =>
    flagValue === undefined ? undefined : flagValue !== false,
  );
  const onFeatureFlags = vi.fn((callback: FeatureFlagsCallback) => {
    featureFlagsCallback = callback;
    return unsubscribe;
  });

  const telemetry: Telemetry = {
    ...nullTelemetry,
    getFeatureFlag,
    isFeatureEnabled: isFeatureEnabled as Telemetry["isFeatureEnabled"],
    onFeatureFlags,
  };

  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <TelemetryStateProvider
        telemetry={telemetry}
        featureFlagsInitiallyAvailable={initiallyAvailable}
      >
        {children}
      </TelemetryStateProvider>
    );
  }

  const hook = renderHook(
    () => useFeatureFlagVariant("gram-risk-llm-analyzer"),
    { wrapper: Wrapper },
  );

  return {
    ...hook,
    getFeatureFlag,
    isFeatureEnabled,
    onFeatureFlags,
    unsubscribe,
    emitFlagResult: (value: FlagValue) => {
      act(() => {
        flagValue = value;
        featureFlagsCallback?.([], {}, { errorsLoading: false });
      });
    },
    emitFlagError: () => {
      act(() => {
        featureFlagsCallback?.([], {}, { errorsLoading: true });
      });
    },
  };
}

describe("useFeatureFlagVariant", () => {
  it("reports loading before PostHog provides flags", () => {
    const { result, getFeatureFlag, isFeatureEnabled, onFeatureFlags } =
      renderFeatureFlagVariant();

    expect(result.current).toEqual({ status: "loading" });
    expect(getFeatureFlag).not.toHaveBeenCalled();
    expect(isFeatureEnabled).not.toHaveBeenCalled();
    expect(onFeatureFlags).toHaveBeenCalledOnce();
  });

  it("reports a fresh variant after flags load", () => {
    const { result, emitFlagResult, getFeatureFlag, isFeatureEnabled } =
      renderFeatureFlagVariant();

    emitFlagResult("shadow");

    expect(result.current).toEqual({
      status: "resolved",
      variant: "shadow",
      enabled: true,
    });
    expect(getFeatureFlag).toHaveBeenCalledWith("gram-risk-llm-analyzer", {
      fresh: true,
    });
    expect(isFeatureEnabled).toHaveBeenCalledWith("gram-risk-llm-analyzer", {
      fresh: true,
    });
  });

  it("keeps the boolean read for a variant named off", () => {
    const { result, emitFlagResult } = renderFeatureFlagVariant();

    emitFlagResult("off");

    expect(result.current).toEqual({
      status: "resolved",
      variant: "off",
      enabled: true,
    });
  });

  it("reports a boolean-only enabled flag without a variant", () => {
    const { result, emitFlagResult } = renderFeatureFlagVariant();

    emitFlagResult(true);

    expect(result.current).toEqual({
      status: "resolved",
      variant: undefined,
      enabled: true,
    });
  });

  it("reports a boolean-only disabled flag without a variant", () => {
    const { result, emitFlagResult } = renderFeatureFlagVariant();

    emitFlagResult(false);

    expect(result.current).toEqual({
      status: "resolved",
      variant: undefined,
      enabled: false,
    });
  });

  it("distinguishes a missing flag from loading", () => {
    const { result, emitFlagResult } = renderFeatureFlagVariant();

    emitFlagResult(undefined);

    expect(result.current).toEqual({ status: "missing" });
  });

  it("reports flag loading errors without using a cached value", () => {
    const { result, emitFlagError, getFeatureFlag, isFeatureEnabled } =
      renderFeatureFlagVariant({ initialValue: "llm" });

    emitFlagError();

    expect(result.current).toEqual({ status: "error" });
    expect(getFeatureFlag).not.toHaveBeenCalled();
    expect(isFeatureEnabled).not.toHaveBeenCalled();
  });

  it("reacts when PostHog reloads a flag", () => {
    const { result, emitFlagResult } = renderFeatureFlagVariant();

    emitFlagResult("shadow");
    expect(result.current).toMatchObject({ variant: "shadow" });

    emitFlagResult("llm");
    expect(result.current).toMatchObject({ variant: "llm" });
  });

  it("uses deterministic telemetry providers without subscribing", () => {
    const { result, onFeatureFlags } = renderFeatureFlagVariant({
      initiallyAvailable: true,
      initialValue: "shadow",
    });

    expect(result.current).toMatchObject({
      status: "resolved",
      variant: "shadow",
    });
    expect(onFeatureFlags).not.toHaveBeenCalled();
  });

  it("unsubscribes the shared listener when the provider unmounts", () => {
    const { unmount, unsubscribe } = renderFeatureFlagVariant();

    unmount();

    expect(unsubscribe).toHaveBeenCalledOnce();
  });
});
