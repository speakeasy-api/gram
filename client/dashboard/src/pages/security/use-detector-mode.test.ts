import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { FeatureFlagVariantResult } from "@/hooks/useFeatureFlagVariant";
import { useDetectorMode } from "./use-detector-mode";

const mocks = vi.hoisted(() => ({
  flagResult: vi.fn(),
}));

vi.mock("@/hooks/useFeatureFlagVariant", () => ({
  useFeatureFlagVariant: () => mocks.flagResult() as FeatureFlagVariantResult,
}));

describe("useDetectorMode", () => {
  it.each<[string, "llm" | "presidio", FeatureFlagVariantResult]>([
    [
      "llm variant",
      "llm",
      { status: "resolved", variant: "llm", enabled: true },
    ],
    [
      "shadow variant",
      "presidio",
      { status: "resolved", variant: "shadow", enabled: true },
    ],
    [
      "off variant, which PostHog still reports as enabled",
      "presidio",
      { status: "resolved", variant: "off", enabled: true },
    ],
    [
      "unknown variant",
      "presidio",
      { status: "resolved", variant: "canary", enabled: true },
    ],
    [
      "boolean flag enabled (transition rule)",
      "llm",
      { status: "resolved", variant: undefined, enabled: true },
    ],
    [
      "boolean flag reported as the string true",
      "llm",
      { status: "resolved", variant: "true", enabled: true },
    ],
    [
      "boolean flag disabled",
      "presidio",
      { status: "resolved", variant: undefined, enabled: false },
    ],
    ["loading", "presidio", { status: "loading" }],
    ["missing", "presidio", { status: "missing" }],
    ["error", "presidio", { status: "error" }],
  ])("%s => %s", (_name, expected, flag) => {
    mocks.flagResult.mockReturnValue(flag);

    const { result } = renderHook(() => useDetectorMode());

    expect(result.current).toBe(expected);
  });
});
