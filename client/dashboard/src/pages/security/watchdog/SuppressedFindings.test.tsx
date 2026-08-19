import type { RiskExclusion } from "@gram/client/models/components/riskexclusion.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { format } from "date-fns";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getRuleTitleFallback } from "../risk-utils";
import { SuppressedFindings } from "./SuppressedFindings";

const restore = vi.fn((_ids: string[]) => Promise.resolve(true));
const openChat = vi.fn();

// The shared chat sheet is a whole transcript UI behind the SDK provider;
// this section's contract with it is just "open this chat id".
vi.mock("@/pages/chatLogs/useChatDetailSheet", () => ({
  useChatDetailSheet: () => ({
    selectedChatId: null,
    openChat,
    sheet: <div data-testid="chat-sheet" />,
  }),
}));

vi.mock("../useDismissFinding", () => ({
  useDismissFinding: () => ({
    restore,
    dismiss: vi.fn(),
    isOptimisticallyDismissed: () => false,
  }),
}));

vi.mock("@gram/client/react-query/riskListDismissedResults.js", () => ({
  useRiskListDismissedResults: (request?: { cursor?: string }) => ({
    data: PAGES[request?.cursor ?? ""],
    isFetching: false,
  }),
}));

vi.mock("@gram/client/react-query/riskListExclusions.js", () => ({
  useRiskListExclusions: () => ({ data: { exclusions: EXCLUSIONS } }),
}));

function makeResult(
  overrides: Partial<RiskResult> & { id: string },
): RiskResult {
  return {
    createdAt: new Date("2026-08-01T12:00:00Z"),
    policyId: "policy-1",
    policyVersion: 1,
    source: "gitleaks",
    ...overrides,
  };
}

const RULE_ROW = makeResult({
  id: "rule-row",
  ruleId: "aws-access-token",
  suppressedReason: "rule",
  exclusionId: "exclusion-1",
  suppressedAt: new Date("2026-08-10T12:00:00Z"),
});

const ORPHAN_RULE_ROW = makeResult({
  id: "orphan-rule-row",
  ruleId: "slack-webhook",
  suppressedReason: "rule",
  // The exclusion was deleted after it suppressed this finding, so the
  // client-side join finds nothing to name.
  exclusionId: "exclusion-gone",
  suppressedAt: new Date("2026-08-10T12:00:00Z"),
});

const MANUAL_ROW = makeResult({
  id: "manual-row",
  ruleId: "generic-api-key",
  suppressedReason: "manual",
  suppressedDetail: "Sample key in our own docs",
  suppressedAt: new Date("2026-08-09T12:00:00Z"),
  chatId: "chat-1",
  chatTitle: "Deploy walkthrough",
  matchRedacted: "<redacted len=32 sha=deadbeef>",
});

const AUTOMATED_ROW = makeResult({
  id: "automated-row",
  ruleId: "private-key",
  suppressedReason: "automated",
  suppressedAt: new Date("2026-08-08T12:00:00Z"),
});

// A rule id of their own so the rows above stay individually addressable by
// their accessible name.
const FILLER_ROWS = Array.from({ length: 6 }, (_, index) =>
  makeResult({
    id: `filler-${index}`,
    ruleId: "filler-rule",
    suppressedReason: "manual",
    suppressedAt: new Date("2026-08-07T12:00:00Z"),
  }),
);

const EXCLUSIONS: RiskExclusion[] = [
  {
    id: "exclusion-1",
    createdAt: new Date("2026-07-01T12:00:00Z"),
    updatedAt: new Date("2026-07-01T12:00:00Z"),
    enabled: true,
    matchType: "rule_id",
    matchValue: "aws-access-token",
    projectId: "project-1",
    ruleIdFilter: "",
    sourceFilter: "",
  },
];

const PAGES: Record<
  string,
  | {
      results: RiskResult[];
      nextCursor?: string;
      totalCount: number;
    }
  | undefined
> = {
  "": {
    results: [
      RULE_ROW,
      ORPHAN_RULE_ROW,
      MANUAL_ROW,
      AUTOMATED_ROW,
      ...FILLER_ROWS,
    ],
    nextCursor: "page-2",
    totalCount: 14,
  },
  "page-2": {
    results: FILLER_ROWS.slice(0, 4).map((row) =>
      makeResult({ ...row, id: `${row.id}-page-2` }),
    ),
    totalCount: 14,
  },
};

function renderSection() {
  // The section hands session viewing off to the shared chat sheet, which
  // owns a delete mutation — hence the real QueryClient rather than a mock.
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/acme/projects/default/watchdog"]}>
        <SuppressedFindings />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function expand() {
  fireEvent.click(screen.getByRole("button", { expanded: false }));
}

/** Clicks the row whose clickable region names this finding. */
function openRow(ruleId: string) {
  fireEvent.click(
    screen.getByRole("button", {
      name: `View suppressed finding ${getRuleTitleFallback(ruleId)}`,
    }),
  );
}

function drawer() {
  return within(screen.getByRole("dialog"));
}

afterEach(cleanup);
beforeEach(() => {
  restore.mockClear();
  openChat.mockClear();
});

describe("SuppressedFindings", () => {
  it("summarizes the suppressed total and disclaims it from the score", () => {
    renderSection();

    expect(screen.getByText("Suppressed · 14")).toBeTruthy();
    expect(screen.getByText("Not in score · Not in counts")).toBeTruthy();
    // Collapsed by default: no rows until the header is clicked.
    expect(screen.queryByText("Suppressed manually")).toBeNull();
  });

  it("renders nothing when the project has no suppressed findings", () => {
    PAGES[""] = { results: [], totalCount: 0 };
    const { container } = renderSection();
    PAGES[""] = {
      results: [
        RULE_ROW,
        ORPHAN_RULE_ROW,
        MANUAL_ROW,
        AUTOMATED_ROW,
        ...FILLER_ROWS,
      ],
      nextCursor: "page-2",
      totalCount: 14,
    };

    expect(container.textContent).toBe("");
  });

  it("names the exclusion behind a rule suppression and links to the rule", () => {
    renderSection();
    expand();

    expect(screen.getByText("aws-access-token")).toBeTruthy();
    expect(screen.getAllByText("Suppressed by rule · Aug 10").length).toBe(2);
    // The deleted exclusion falls back to a generic label rather than a blank
    // provenance line.
    expect(screen.getByText("Exclusion rule")).toBeTruthy();

    // Styled as a button (same action slot as Restore); navigation happens
    // via router navigate on click rather than an anchor href.
    const viewRule = screen.getAllByRole("button", { name: "View rule" });
    expect(viewRule.length).toBe(2);
  });

  it("shows the dismissal note for a manual suppression and the label alone for an automated one", () => {
    renderSection();
    expand();

    expect(screen.getByText("Sample key in our own docs")).toBeTruthy();
    expect(screen.getByText("Suppressed manually · Aug 9")).toBeTruthy();
    expect(screen.getByText("Suppressed automatically · Aug 8")).toBeTruthy();
    expect(screen.getByText(getRuleTitleFallback("private-key"))).toBeTruthy();
  });

  it("restores a single manual finding", () => {
    renderSection();
    expand();

    // Rule rows carry no restore, so every Restore button belongs to a
    // manual/automated row: 10 rows on this page minus the 2 rule rows.
    const restoreButtons = screen.getAllByRole("button", { name: "Restore" });
    expect(restoreButtons.length).toBe(8);

    fireEvent.click(restoreButtons[0]!);
    expect(restore).toHaveBeenCalledWith(["manual-row"]);
  });

  it("bulk-restores the selection without sweeping in rule rows", () => {
    renderSection();
    expand();

    fireEvent.click(
      screen.getByLabelText("Select all restorable findings", {
        selector: "[role=checkbox]",
      }),
    );
    // The toolbar's Restore is the first one; the row buttons follow.
    fireEvent.click(screen.getAllByRole("button", { name: "Restore" })[0]!);

    const ids = restore.mock.calls[0]?.[0] as string[];
    expect(ids).toHaveLength(8);
    expect(ids).toContain("manual-row");
    expect(ids).toContain("automated-row");
    expect(ids).not.toContain("rule-row");
  });

  it("opens the detail drawer from a row click and shows the provenance", () => {
    renderSection();
    expand();
    expect(screen.queryByRole("dialog")).toBeNull();

    openRow("generic-api-key");

    const panel = drawer();
    expect(
      panel.getByText(getRuleTitleFallback("generic-api-key")),
    ).toBeTruthy();
    expect(panel.getByText("Suppressed manually")).toBeTruthy();
    expect(panel.getByText("Sample key in our own docs")).toBeTruthy();
    // Full timestamp in the drawer, not the row's short date. Formatted here
    // rather than hardcoded so the assertion doesn't depend on the local zone.
    expect(
      panel.getByText(format(MANUAL_ROW.suppressedAt!, "MMM d, yyyy h:mm a")),
    ).toBeTruthy();
  });

  it("offers restore in the drawer for a manual finding, and no suppression actions", () => {
    renderSection();
    expand();
    openRow("generic-api-key");

    const panel = drawer();
    expect(panel.getByRole("button", { name: "Restore" })).toBeTruthy();
    // This finding is already suppressed, so the active-signal drawer's
    // actions must not appear here.
    expect(panel.queryByRole("button", { name: /exclusion/i })).toBeNull();
    expect(panel.queryByRole("button", { name: /suppress/i })).toBeNull();

    fireEvent.click(panel.getByRole("button", { name: "Restore" }));
    expect(restore).toHaveBeenCalledWith(["manual-row"]);
  });

  it("offers view-rule in the drawer for a rule-suppressed finding", () => {
    renderSection();
    expand();
    openRow("aws-access-token");

    const panel = drawer();
    expect(panel.getByRole("button", { name: "View rule" })).toBeTruthy();
    expect(panel.queryByRole("button", { name: "Restore" })).toBeNull();
    expect(panel.getByText("Suppressed by rule")).toBeTruthy();
    // The provenance is labeled as the exclusion, and its match value —
    // resolved through the exclusions listing — is shown.
    expect(panel.getByText("Exclusion rule")).toBeTruthy();
    expect(panel.getAllByText("aws-access-token").length).toBeGreaterThan(0);
  });

  it("hands the session off to the chat sheet instead of stacking drawers", () => {
    renderSection();
    expand();
    openRow("generic-api-key");

    fireEvent.click(drawer().getByRole("button", { name: "View session" }));

    expect(openChat).toHaveBeenCalledWith("chat-1");
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("shows the redacted match without a reveal the server would refuse", () => {
    renderSection();
    expand();
    openRow("generic-api-key");

    const panel = drawer();
    expect(panel.getByText("<redacted len=32 sha=deadbeef>")).toBeTruthy();
    expect(panel.queryByText("Click to reveal")).toBeNull();
  });

  it("leaves the drawer closed when the row's own controls are clicked", () => {
    renderSection();
    expand();

    fireEvent.click(screen.getAllByRole("button", { name: "Restore" })[0]!);
    expect(screen.queryByRole("dialog")).toBeNull();

    fireEvent.click(
      screen.getAllByLabelText("Select suppressed finding", {
        selector: "[role=checkbox]",
      })[0]!,
    );
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("pages forward through the cursor and reports the range", () => {
    renderSection();
    expand();

    expect(screen.getByText("1–10 of 14 suppressed")).toBeTruthy();
    const previous = screen.getByRole("button", { name: "Previous" });
    expect(previous.hasAttribute("disabled")).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Next" }));

    expect(screen.getByText("11–14 of 14 suppressed")).toBeTruthy();
    // The last page has no cursor to follow.
    expect(
      screen.getByRole("button", { name: "Next" }).hasAttribute("disabled"),
    ).toBe(true);
    expect(
      screen.getByRole("button", { name: "Previous" }).hasAttribute("disabled"),
    ).toBe(false);
  });
});
