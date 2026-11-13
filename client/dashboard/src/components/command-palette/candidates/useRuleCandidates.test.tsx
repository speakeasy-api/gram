import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useBuiltinRuleCandidates } from "./useRuleCandidates";

vi.mock("@/pages/security/detection-rules-data", () => ({
  BUILTIN_RULES_BY_CATEGORY: {
    access: [
      { id: "builtin.access", title: "Built in", defaultSeverity: "high" },
    ],
  },
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project-one",
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ policyCenter: { href: () => "/guardrails" } }),
}));
vi.mock("react-router", () => ({ useNavigate: () => vi.fn() }));
vi.mock("@gram/client/react-query/riskListCustomDetectionRules.js", () => ({
  useRiskListCustomDetectionRules: () => ({
    // React Query can return cached rows even while this query is disabled.
    data: {
      rules: [
        {
          id: "row-1",
          ruleId: "custom.duplicate",
          title: "Cached custom rule",
          severity: "medium",
        },
      ],
    },
  }),
}));

describe("useBuiltinRuleCandidates", () => {
  it("filters cached custom rows to prevent duplicate candidates", () => {
    const { result } = renderHook(() =>
      useBuiltinRuleCandidates({ enabled: true }),
    );
    expect(result.current.map((candidate) => candidate.id)).toEqual([
      "rule:builtin.access",
    ]);
  });
});
