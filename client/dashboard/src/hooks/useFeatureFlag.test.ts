import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, expectTypeOf, it, vi } from "vitest";
import { useFeatureFlag } from "./useFeatureFlag";

const testState = vi.hoisted(() => ({
  flagValue: undefined as boolean | undefined,
  lastFlag: undefined as string | undefined,
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({
    isFeatureEnabled: (flag: string) => {
      testState.lastFlag = flag;
      return testState.flagValue;
    },
  }),
}));

afterEach(() => {
  testState.flagValue = undefined;
  testState.lastFlag = undefined;
});

describe("useFeatureFlag", () => {
  it("treats an unresolved flag as off in off mode", () => {
    const { result } = renderHook(() => useFeatureFlag("gram-rbac", "off"));

    expect(result.current).toBe(false);
    expect(testState.lastFlag).toBe("gram-rbac");
  });

  it("defaults to off mode", () => {
    const { result } = renderHook(() => useFeatureFlag("gram-rbac"));

    expect(result.current).toBe(false);
  });

  it("treats an unresolved flag as on in on mode", () => {
    const { result } = renderHook(() => useFeatureFlag("gram-rbac", "on"));

    expect(result.current).toBe(true);
  });

  it("preserves an unresolved flag in unresolved mode", () => {
    const { result } = renderHook(() =>
      useFeatureFlag("gram-rbac", "unresolved"),
    );

    expect(result.current).toBeUndefined();
  });

  it.each([true, false])(
    "returns the resolved value %s regardless of loading mode",
    (flagValue) => {
      testState.flagValue = flagValue;

      const { result } = renderHook(() =>
        useFeatureFlag("gram-rbac", "unresolved"),
      );

      expect(result.current).toBe(flagValue);
    },
  );

  it("exposes mode-specific return types", () => {
    function useOffResult() {
      return useFeatureFlag("gram-rbac", "off");
    }
    function useUnresolvedResult() {
      return useFeatureFlag("gram-rbac", "unresolved");
    }

    expectTypeOf(useOffResult).returns.toEqualTypeOf<boolean>();
    expectTypeOf(useUnresolvedResult).returns.toEqualTypeOf<
      boolean | undefined
    >();
  });
});
