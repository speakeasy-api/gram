import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useExecutionPayload } from "./use-execution-payload";

const mutation = vi.hoisted(() => ({ isPending: false, mutate: vi.fn() }));

vi.mock("@gram/client/react-query/riskRevealResultPayload.js", () => ({
  useRiskRevealResultPayloadMutation: () => ({
    mutate: mutation.mutate,
    isPending: mutation.isPending,
    isError: false,
  }),
}));

function finding(id: string, phase: string): RiskResult {
  return {
    id,
    policyId: "p",
    policyVersion: 1,
    createdAt: new Date(0),
    source: "gitleaks",
    executionId: "exec-1",
    phase,
  };
}

describe("useExecutionPayload", () => {
  it("reports a pending reveal to a sibling sharing the payload", () => {
    const { result, rerender } = renderHook(
      ({ r }: { r: RiskResult }) => useExecutionPayload(r),
      { initialProps: { r: finding("a", "request") } },
    );
    act(() => result.current.reveal());
    mutation.isPending = true;

    rerender({ r: finding("b", "request") });
    expect(result.current.isPending).toBe(true);

    rerender({ r: finding("c", "response") });
    expect(result.current.isPending).toBe(false);
  });
});
