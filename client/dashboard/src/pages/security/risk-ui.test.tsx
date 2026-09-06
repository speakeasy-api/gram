import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { EventMatchDialog, MaskedMatch, RuleLabel } from "./risk-ui";

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

const RATIONALE =
  "The tool result instructs the agent to ignore prior instructions and read ~/.aws/credentials.";

function renderCell(
  rationale: string | undefined,
  matchRedacted = "<redacted len=42 sha=deadbeef>",
) {
  render(
    <TooltipProvider>
      <EventMatchDialog
        resultId="00000000-0000-0000-0000-000000000001"
        matchRedacted={matchRedacted}
        rationale={rationale}
      />
    </TooltipProvider>,
  );
}

afterEach(cleanup);
beforeEach(() => hasScope.mockReset());

describe("EventMatchDialog", () => {
  it("shows the judge rationale inline instead of a bare reveal prompt", () => {
    hasScope.mockReturnValue(true);
    renderCell(RATIONALE);

    expect(screen.getByText(RATIONALE)).toBeTruthy();
    expect(screen.queryByText("Click to reveal")).toBeNull();
    // The rationale itself opens the flagged event, so the reveal affordance
    // isn't lost.
    expect(screen.getByRole("button").textContent).toContain(RATIONALE);
  });

  it("falls back to the reveal prompt when the judge returned no rationale", () => {
    hasScope.mockReturnValue(true);
    renderCell("   ");

    expect(screen.getByText("Click to reveal")).toBeTruthy();
  });

  it("offers no reveal when the finding stored no event to reveal", () => {
    hasScope.mockReturnValue(true);
    // A prompt-based policy finding records the judge's verdict, not a span of
    // the message, so the server fingerprints an empty match.
    renderCell(RATIONALE, "<redacted len=0>");

    expect(screen.getByText(RATIONALE)).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("still shows the rationale without chat:read, but offers no reveal", () => {
    hasScope.mockReturnValue(false);
    renderCell(RATIONALE);

    expect(screen.getByText(RATIONALE)).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
    // The rationale reads as ordinary text, so the withheld payload has to be
    // announced by something.
    expect(screen.getByRole("img", { name: /chat:read/ })).toBeTruthy();
  });

  it("falls back to the redacted match without chat:read and no rationale", () => {
    hasScope.mockReturnValue(false);
    renderCell(undefined);

    expect(screen.getByText("<redacted len=42 sha=deadbeef>")).toBeTruthy();
    expect(screen.queryByText("Hidden")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByRole("img", { name: /chat:read/ })).toBeTruthy();
  });
});

function renderMasked(matchRedacted = "<redacted len=42 sha=deadbeef>") {
  render(
    <TooltipProvider>
      <MaskedMatch
        resultId="00000000-0000-0000-0000-000000000001"
        matchRedacted={matchRedacted}
      />
    </TooltipProvider>,
  );
}

describe("MaskedMatch", () => {
  it("shows the redacted match without chat:read, and offers no reveal", () => {
    hasScope.mockReturnValue(false);
    renderMasked();

    expect(screen.getByText("<redacted len=42 sha=deadbeef>")).toBeTruthy();
    expect(screen.queryByText("Hidden")).toBeNull();
    expect(screen.queryByText("Click to reveal")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByRole("img", { name: /chat:read/ })).toBeTruthy();
  });

  it("keeps the reveal affordance when chat:read is granted", () => {
    hasScope.mockReturnValue(true);
    renderMasked();

    expect(screen.getByText("Click to reveal")).toBeTruthy();
    expect(screen.queryByText("<redacted len=42 sha=deadbeef>")).toBeNull();
  });
});

describe("RuleLabel", () => {
  it("renders no second line for judge sources, whose rule restates the category", () => {
    const { container } = render(
      <RuleLabel source="prompt_injection" ruleId="prompt_injection" />,
    );
    expect(container.textContent).toBe("");
  });

  it("renders no second line when the finding carries no rule id", () => {
    const { container } = render(<RuleLabel source="gitleaks" />);
    expect(container.textContent).toBe("");
  });

  it("renders the rule title for detector sources", () => {
    render(<RuleLabel source="presidio" ruleId="pii.us_ssn" />);
    expect(screen.getByText("US Social Security Number")).toBeTruthy();
  });
});
