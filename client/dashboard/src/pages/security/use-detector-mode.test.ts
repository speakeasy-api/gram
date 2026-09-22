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
  it.each<[string, FeatureFlagVariantResult, "llm" | "presidio"]>([
    [
      "llm variant",
      { status: "resolved", variant: "llm", enabled: true },
      "llm",
    ],
    [
      "shadow variant",
      { status: "resolved", variant: "shadow", enabled: true },
      "presidio",
    ],
    [
      "off variant, which PostHog still reports as enabled",
      { status: "resolved", variant: "off", enabled: true },
      "presidio",
    ],
    [
      "unknown variant",
      { status: "resolved", variant: "canary", enabled: true },
      "presidio",
    ],
    [
      "boolean flag enabled (transition rule)",
      { status: "resolved", variant: undefined, enabled: true },
      "llm",
    ],
    [
      "boolean flag reported as the string true",
      { status: "resolved", variant: "true", enabled: true },
      "llm",
    ],
    [
      "boolean flag disabled",
      { status: "resolved", variant: undefined, enabled: false },
      "presidio",
    ],
    ["loading", { status: "loading" }, "presidio"],
    ["missing", { status: "missing" }, "presidio"],
    ["error", { status: "error" }, "presidio"],
  ])("%s => %s", (_name, flag, expected) => {
    mocks.flagResult.mockReturnValue(flag);

    const { result } = renderHook(() => useDetectorMode());

    expect(result.current).toBe(expected);
  });
});
