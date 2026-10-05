import { TooltipProvider } from "@/components/ui/Tooltip";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { FindingPayload } from "./FindingPayload";
import { STORED_PAYLOAD_MAX_BYTES } from "./payload-spans";

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));

vi.mock("@gram/client/react-query/riskUnmaskResult.js", () => ({
  useRiskUnmaskResultMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));

const CUT_NOTE =
  "The stored payload stops at 64 KB; this match is past the cut.";
// A payload the server cut at the cap, with the match starting just before it.
const PAYLOAD = "a".repeat(STORED_PAYLOAD_MAX_BYTES);

function finding(startPos: number, endPos: number): RiskResult {
  return {
    id: "00000000-0000-0000-0000-000000000001",
    policyId: "p1",
    policyVersion: 1,
    createdAt: new Date("2026-09-16T00:00:00Z"),
    source: "gitleaks",
    ruleId: "secret.token",
    matchRedacted: "<redacted len=20 sha=0123abcd>",
    executionId: "exec-1",
    phase: "response",
    startPos,
    endPos,
  };
}

function renderPayload(result: RiskResult, revealed: boolean) {
  render(
    <TooltipProvider>
      <FindingPayload
        result={result}
        siblings={[]}
        siblingsComplete
        revealed={revealed}
        canReveal
        payload={{
          data: {
            id: "00000000-0000-0000-0000-000000000001",
            revealState: "available",
            payload: PAYLOAD,
            expiresAt: new Date("2026-12-15T00:00:00Z"),
          },
          isPending: false,
          isError: false,
          reveal: vi.fn<() => void>(),
        }}
        onToggleReveal={vi.fn<() => void>()}
        onSelect={vi.fn<(id: string) => void>()}
      />
    </TooltipProvider>,
  );
}

afterEach(cleanup);

describe("FindingPayload with a match past the stored cut", () => {
  it("keeps the masked view on the match and explains the cut", () => {
    renderPayload(
      finding(STORED_PAYLOAD_MAX_BYTES - 10, STORED_PAYLOAD_MAX_BYTES + 10),
      false,
    );

    expect(screen.getByText(CUT_NOTE)).toBeTruthy();
    expect(document.body.textContent).not.toContain("aaaaaaaaaa");
  });

  it("shows the revealed payload with the cut note", () => {
    renderPayload(
      finding(STORED_PAYLOAD_MAX_BYTES - 10, STORED_PAYLOAD_MAX_BYTES + 10),
      true,
    );

    expect(screen.getByText(CUT_NOTE)).toBeTruthy();
    expect(document.body.textContent).toContain("aaaaaaaaaa");
  });

  it("shows no note for a match inside the payload", () => {
    renderPayload(finding(0, 10), false);

    expect(screen.queryByText(CUT_NOTE)).toBeNull();
  });
});
