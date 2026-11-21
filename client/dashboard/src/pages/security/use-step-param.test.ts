import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  type PolicyStep,
  unvisitedStepsReason,
  useVisitedSteps,
} from "./use-step-param";

vi.mock("nuqs", () => ({ useQueryState: vi.fn() }));

const STEPS: PolicyStep[] = [
  { id: "detect", title: "Detect", description: "" },
  { id: "scope", title: "Scope", description: "" },
  { id: "action", title: "Action", description: "" },
  { id: "review", title: "Review", description: "" },
];

describe("useVisitedSteps", () => {
  it("starts with the step the editor mounted on", () => {
    const { result } = renderHook(({ step }) => useVisitedSteps(step), {
      initialProps: { step: 3 },
    });
    expect([...result.current]).toEqual([3]);
  });

  it("remembers every step opened, including after going back", () => {
    const { result, rerender } = renderHook(
      ({ step }) => useVisitedSteps(step),
      { initialProps: { step: 0 } },
    );
    rerender({ step: 1 });
    rerender({ step: 2 });
    rerender({ step: 1 });
    expect([...result.current].sort()).toEqual([0, 1, 2]);
  });
});

describe("unvisitedStepsReason", () => {
  it("blocks create from Scope while Action and Review are unseen", () => {
    expect(unvisitedStepsReason(new Set([0, 1]), STEPS)).toBe(
      "Check Action and Review to create",
    );
  });

  it("names a single remaining step", () => {
    expect(unvisitedStepsReason(new Set([0, 1, 2]), STEPS)).toBe(
      "Check Review to create",
    );
  });

  it("lists every unseen step for a deep link to Review", () => {
    expect(unvisitedStepsReason(new Set([3]), STEPS)).toBe(
      "Check Detect, Scope and Action to create",
    );
  });

  it("allows create once every step has been opened", () => {
    expect(unvisitedStepsReason(new Set([0, 1, 2, 3]), STEPS)).toBeUndefined();
  });
});
