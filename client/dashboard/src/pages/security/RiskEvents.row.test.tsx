import { TooltipProvider } from "@/components/ui/Tooltip";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RiskEventsRow } from "./RiskEvents";

const hasScope = vi.fn<(scope: string) => boolean>();

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope }),
}));

vi.mock("@gram/client/react-query/riskUnmaskResult.js", () => ({
  useRiskUnmaskResultMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));

vi.mock("@/components/code", () => ({
  CodeBlock: ({ children }: { children: string }) => <pre>{children}</pre>,
}));

// The identity link resolves its href through the router and project context,
// neither of which a single row needs.
vi.mock("@/components/identity-link", () => ({
  IdentityLink: ({ children }: { children: ReactNode }) => (
    <span>{children}</span>
  ),
}));

const REASONING =
  "The tool output pastes a live cloud access key next to its secret.";

function finding(overrides: Partial<RiskResult> = {}): RiskResult {
  return {
    id: "00000000-0000-0000-0000-000000000001",
    policyId: "p1",
    policyVersion: 1,
    createdAt: new Date("2026-09-16T00:00:00Z"),
    source: "llm_analyzer",
    ruleId: "secret.llm",
    description: REASONING,
    confidence: 1,
    // The analyzer reports no spans: the server fingerprints the empty match
    // to the no-match sentinel.
    matchRedacted: "<redacted len=0>",
    chatTitle: "Deploy the new service",
    userId: "user-key",
    ...overrides,
  };
}

function renderRow(result: RiskResult) {
  render(
    <TooltipProvider>
      <RiskEventsRow
        result={result}
        policyName="Secrets policy"
        policyScore={9.1}
        onSelectChat={vi.fn<(chatId: string | null) => void>()}
        selection={{
          selectedIds: new Set(),
          selectedCount: 0,
          isSelected: () => false,
          toggle: vi.fn<(id: string) => void>(),
          toggleAll: vi.fn<() => void>(),
          clear: vi.fn<() => void>(),
          allState: false,
          selectedItems: [],
        }}
        onDismiss={vi.fn<(result: RiskResult) => void>()}
        onSetupExclusion={vi.fn<(result: RiskResult) => void>()}
      />
    </TooltipProvider>,
  );
}

afterEach(cleanup);
beforeEach(() => hasScope.mockReset());

describe("RiskEventsRow with an LLM analyzer finding", () => {
  it("shows the category, the rule as its category name, and the reasoning", () => {
    hasScope.mockReturnValue(true);
    renderRow(finding());

    expect(screen.getByText("Secrets")).toBeTruthy();
    expect(screen.getByText("Secret")).toBeTruthy();
    expect(screen.getByText(REASONING)).toBeTruthy();
    // Nothing on the row names the engine or exposes the raw id/fingerprint.
    expect(document.body.textContent).not.toMatch(/llm|analyzer|model/i);
    expect(screen.queryByText("<redacted len=0>")).toBeNull();
  });

  it("offers no reveal, since the finding stores no match", () => {
    hasScope.mockReturnValue(true);
    renderRow(finding());

    expect(screen.queryByText("Click to reveal")).toBeNull();
    expect(screen.queryByText("Revealing…")).toBeNull();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("keeps the reasoning visible without chat:read", () => {
    hasScope.mockReturnValue(false);
    renderRow(finding());

    expect(screen.getByText(REASONING)).toBeTruthy();
    expect(screen.queryByText("Click to reveal")).toBeNull();
  });

  it("renders the dead-letter sentinel as unavailable analysis", () => {
    hasScope.mockReturnValue(true);
    renderRow(
      finding({
        ruleId: "llm_analyzer.dead_letter",
        description: "The risk analysis could not be completed.",
      }),
    );

    expect(screen.getByText("Analysis unavailable")).toBeTruthy();
    expect(
      screen.getByText("The risk analysis could not be completed."),
    ).toBeTruthy();
    expect(document.body.textContent).not.toMatch(/llm|analyzer/i);
  });
});
