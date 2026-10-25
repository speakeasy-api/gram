import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import {
  nullTelemetry,
  TelemetryStateProvider,
  type Telemetry,
} from "@/contexts/Telemetry";
import { useFeatureFlag } from "./useFeatureFlag";
import { FEATURE_FLAGS, type FeatureFlag } from "@/lib/featureFlags";
import { failOpenMissingFlags } from "@/contexts/TelemetryProvider";

type FeatureFlagsCallback = Parameters<Telemetry["onFeatureFlags"]>[0];
type FeatureFlagOptions = Parameters<Telemetry["isFeatureEnabled"]>[1];
type FeatureFlagOptionsWithDefault = FeatureFlagOptions & {
  defaultValue: boolean;
};

function renderFeatureFlag({
  initiallyAvailable = false,
  initialValue,
  flag = FEATURE_FLAGS.explore,
  preview = false,
}: {
  initiallyAvailable?: boolean;
  initialValue?: boolean;
  flag?: FeatureFlag;
  preview?: boolean;
} = {}) {
  let flagValue = initialValue;
  let featureFlagsCallback: FeatureFlagsCallback | undefined;
  const unsubscribe = vi.fn();
  const isFeatureEnabled = vi.fn(
    (_flag: string, options?: FeatureFlagOptions) =>
      flagValue ?? options?.defaultValue,
  );
  const onFeatureFlags = vi.fn((callback: FeatureFlagsCallback) => {
    featureFlagsCallback = callback;
    return unsubscribe;
  });

  function telemetryIsFeatureEnabled(
    flag: string,
    options: FeatureFlagOptionsWithDefault,
  ): boolean;
  function telemetryIsFeatureEnabled(
    flag: string,
    options?: FeatureFlagOptions,
  ): boolean | undefined;
  function telemetryIsFeatureEnabled(
    flag: string,
    options?: FeatureFlagOptions,
  ): boolean | undefined {
    return isFeatureEnabled(flag, options);
  }

  const telemetry: Telemetry = {
    ...nullTelemetry,
    isFeatureEnabled: telemetryIsFeatureEnabled,
    onFeatureFlags,
  };
  const providerTelemetry = preview
    ? failOpenMissingFlags(telemetry)
    : telemetry;

  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <TelemetryStateProvider
        telemetry={providerTelemetry}
        featureFlagsInitiallyAvailable={initiallyAvailable}
      >
        {children}
      </TelemetryStateProvider>
    );
  }

  const hook = renderHook(() => useFeatureFlag(flag), {
    wrapper: Wrapper,
  });

  return {
    ...hook,
    isFeatureEnabled,
    onFeatureFlags,
    unsubscribe,
    emitFlagResult: (value: boolean | undefined) => {
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

describe("useFeatureFlag", () => {
  it("keeps missing Debug unavailable through the preview provider", () => {
    const { result, emitFlagResult } = renderFeatureFlag({
      flag: FEATURE_FLAGS.gatewayDebug,
      preview: true,
    });

    emitFlagResult(undefined);
    expect(result.current).toEqual({ status: "missing" });
    emitFlagResult(true);
    expect(result.current).toEqual({ status: "enabled" });
    emitFlagResult(false);
    expect(result.current).toEqual({ status: "disabled" });
    emitFlagResult(undefined);
    expect(result.current).toEqual({ status: "missing" });
  });

  it("reports loading before PostHog provides flags", () => {
    const { result, isFeatureEnabled, onFeatureFlags } = renderFeatureFlag();

    expect(result.current).toEqual({
      status: "loading",
    });
    expect(isFeatureEnabled).not.toHaveBeenCalled();
    expect(onFeatureFlags).toHaveBeenCalledOnce();
  });

  it("reports a fresh enabled value after flags load", () => {
    const { result, emitFlagResult, isFeatureEnabled } = renderFeatureFlag();

    emitFlagResult(true);

    expect(result.current).toEqual({ status: "enabled" });
    expect(isFeatureEnabled).toHaveBeenCalledWith(FEATURE_FLAGS.explore, {
      fresh: true,
    });
  });

  it("reports a fresh disabled value after flags load", () => {
    const { result, emitFlagResult } = renderFeatureFlag();

    emitFlagResult(false);

    expect(result.current).toEqual({ status: "disabled" });
  });

  it("distinguishes a missing flag from loading", () => {
    const { result, emitFlagResult } = renderFeatureFlag();

    emitFlagResult(undefined);

    expect(result.current).toEqual({
      status: "missing",
    });
  });

  it("reports flag loading errors without using a cached value", () => {
    const { result, emitFlagError, isFeatureEnabled } = renderFeatureFlag({
      initialValue: true,
    });

    emitFlagError();

    expect(result.current).toEqual({
      status: "error",
    });
    expect(isFeatureEnabled).not.toHaveBeenCalled();
  });

  it("reacts when PostHog reloads a flag", () => {
    const { result, emitFlagResult } = renderFeatureFlag();

    emitFlagResult(true);
    expect(result.current).toEqual({ status: "enabled" });

    emitFlagResult(false);
    expect(result.current).toEqual({ status: "disabled" });
  });

  it("uses deterministic telemetry providers without subscribing", () => {
    const { result, onFeatureFlags } = renderFeatureFlag({
      initiallyAvailable: true,
      initialValue: true,
    });

    expect(result.current).toEqual({ status: "enabled" });
    expect(onFeatureFlags).not.toHaveBeenCalled();
  });

  it("unsubscribes the shared listener when the provider unmounts", () => {
    const { unmount, unsubscribe } = renderFeatureFlag();

    unmount();

    expect(unsubscribe).toHaveBeenCalledOnce();
  });
});
