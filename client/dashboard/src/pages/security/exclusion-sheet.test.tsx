import { TooltipProvider } from "@/components/ui/Tooltip";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ExclusionEditor } from "./exclusion-sheet";

const access = vi.hoisted(() => ({ restricted: false }));
vi.mock("./use-risk-content-restricted", () => ({
  useRiskContentRestricted: () => access.restricted,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("./use-detector-mode", () => ({
  useDetectorMode: () => "presidio",
}));
vi.mock("./use-cel-engine", () => ({
  useCelEngine: () => ({ status: "loading" }),
}));
vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  useRiskListPolicies: () => ({ data: { policies: [] } }),
}));
vi.mock("@gram/client/react-query/riskCreateExclusion.js", () => ({
  useRiskCreateExclusionMutation: () => ({ isPending: false }),
}));
vi.mock("@gram/client/react-query/riskUpdateExclusion.js", () => ({
  useRiskUpdateExclusionMutation: () => ({ isPending: false }),
}));
vi.mock("@gram/client/react-query/riskSuggestExclusion.js", () => ({
  useRiskSuggestExclusionMutation: () => ({ isPending: false }),
}));
vi.mock("@gram/client/react-query/riskUnmaskResult.js", () => ({
  useRiskUnmaskResultMutation: () => ({ isPending: false }),
}));

afterEach(cleanup);
beforeEach(() => {
  access.restricted = false;
});

function renderEditor(match?: string) {
  const result: RiskResult = {
    id: "finding-1",
    policyId: "policy-1",
    policyVersion: 1,
    chatId: "chat-1",
    source: "gitleaks",
    ruleId: "generic-api-key",
    match,
    matchRedacted: "<redacted len=42 sha=deadbeef>",
    createdAt: new Date(),
  };
  render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <ExclusionEditor
          state={{ mode: "create", results: [result] }}
          onDone={() => {}}
        />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("exact-value exclusion during impersonation", () => {
  it("offers an audited exact-value reveal for ordinary viewers", () => {
    renderEditor();
    expect(screen.getByText(/^Just this value/)).toBeTruthy();
  });

  it.each([undefined, "synthetic-secret"])(
    "omits the exact option for restricted viewers, including cached plaintext (%s)",
    (match) => {
      access.restricted = true;
      renderEditor(match);
      expect(screen.queryByText(/^Just this value/)).toBeNull();
    },
  );
});
