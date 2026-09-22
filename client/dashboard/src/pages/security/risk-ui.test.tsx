import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  CategoryLabel,
  EventMatchDialog,
  MaskedMatch,
  RuleLabel,
} from "./risk-ui";

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

  it("names the missing permission without chat:read and no rationale", async () => {
    hasScope.mockReturnValue(false);
    renderCell(undefined);

    expect(screen.getByText("chat:read")).toBeTruthy();
    // The fingerprint moves to the tooltip, off the page until hovered.
    expect(screen.queryByText("<redacted len=42 sha=deadbeef>")).toBeNull();
    await expectFingerprintInTooltip();
    expect(screen.queryByText("Hidden")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByRole("img", { name: /chat:read/ })).toBeTruthy();
  });

  it("shows the LLM analyzer's reasoning with no reveal, since it stores no match", () => {
    hasScope.mockReturnValue(true);
    const reasoning =
      "The tool output contains an AWS access key id followed by its secret.";
    // The analyzer reports no spans, so the server fingerprints the empty
    // match to the no-match sentinel — same shape as a judge verdict.
    renderCell(reasoning, "<redacted len=0>");

    expect(screen.getByText(reasoning)).toBeTruthy();
    expect(screen.queryByText("Click to reveal")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.queryByText("<redacted len=0>")).toBeNull();
  });
});

// Focus rather than hover: the trigger is focusable so keyboard users can
// reach the fingerprint, and focus opens the tooltip without pointer events.
async function expectFingerprintInTooltip() {
  const trigger = screen.getByText("chat:read").closest("[tabindex]");
  expect(trigger).toBeTruthy();
  fireEvent.focus(trigger!);
  expect(
    (await screen.findAllByText("<redacted len=42 sha=deadbeef>")).length,
  ).toBeGreaterThan(0);
}

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
  it("names the missing permission without chat:read, and offers no reveal", async () => {
    hasScope.mockReturnValue(false);
    renderMasked();

    expect(screen.getByText("chat:read")).toBeTruthy();
    expect(screen.queryByText("<redacted len=42 sha=deadbeef>")).toBeNull();
    await expectFingerprintInTooltip();
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

  it.each([
    ["secret.llm", "Secret"],
    ["pii.llm", "PII"],
    ["prompt_injection.llm", "Prompt injection"],
    ["destructive_tool.llm", "Destructive tool"],
    ["cli_destructive.llm", "Destructive command"],
    ["llm_analyzer.dead_letter", "Analysis unavailable"],
  ])(
    "renders %s from the LLM analyzer as %s, never the raw id",
    (ruleId, label) => {
      render(<RuleLabel source="llm_analyzer" ruleId={ruleId} />);
      const line = screen.getByText(label);
      expect(line.getAttribute("title")).toBe(label);
      expect(document.body.textContent).not.toContain(ruleId);
    },
  );
});

describe("CategoryLabel", () => {
  it.each([
    ["secret.llm", "Secrets"],
    ["pii.llm", "Personal Identifiable Information"],
    ["prompt_injection.llm", "Prompt Injection"],
    ["destructive_tool.llm", "Destructive Tools"],
    ["cli_destructive.llm", "Destructive CLI Commands"],
  ])("classifies the LLM analyzer's %s under %s", (ruleId, label) => {
    render(
      <TooltipProvider>
        <CategoryLabel source="llm_analyzer" ruleId={ruleId} />
      </TooltipProvider>,
    );
    expect(screen.getByText(label)).toBeTruthy();
    expect(document.body.textContent).not.toMatch(/llm/i);
  });
});
