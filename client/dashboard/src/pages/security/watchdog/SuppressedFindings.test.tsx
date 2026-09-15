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

/** Ids the hook is optimistically treating as already restored. Expiry of
 * these entries is the hook's own business — see useDismissFinding.test.tsx. */
const optimisticallyRestored = new Set<string>();

vi.mock("../useDismissFinding", () => ({
  useDismissFinding: () => ({
    restore,
    dismiss: vi.fn(),
    isOptimisticallyDismissed: () => false,
    optimisticallyRestoredIds: optimisticallyRestored,
  }),
}));

/** Drives the listing query's non-data state (loading/stale/error) per test. */
const listState = {
  isPlaceholderData: false,
  isFetching: false,
  isError: false,
};
const refetch = vi.fn();

vi.mock("@gram/client/react-query/riskListDismissedResults.js", () => ({
  useRiskListDismissedResults: (request?: { cursor?: string }) => ({
    data: listState.isError ? undefined : PAGES[request?.cursor ?? ""],
    isFetching: listState.isFetching,
    isPlaceholderData: listState.isPlaceholderData,
    isError: listState.isError,
    refetch,
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
  matchRedacted: "<redacted len=20 sha=abc12345>",
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
  // A prompt-based finding records the judge's verdict, not a span of the
  // message, so the server fingerprints an empty match.
  matchRedacted: "<redacted len=0>",
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

const SHADOW_MCP_ROW = makeResult({
  id: "shadow-row",
  ruleId: "unapproved-server",
  source: "shadow_mcp",
  suppressedReason: "manual",
  suppressedAt: new Date("2026-08-06T12:00:00Z"),
  // Documented carve-out: shadow MCP matches are server identifiers, not
  // captured content, so the server passes match_redacted through verbatim.
  matchRedacted: "https://mcp.example.com/sse",
});

// A shadow_mcp row written before the passthrough carve-out landed: same
// source, but the value is still a fingerprint.
const LEGACY_SHADOW_ROW = makeResult({
  id: "legacy-shadow-row",
  ruleId: "legacy-server",
  source: "shadow_mcp",
  suppressedReason: "manual",
  suppressedAt: new Date("2026-08-05T12:00:00Z"),
  matchRedacted: "<redacted len=31 sha=0f1e2d3c>",
});

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

type Page = {
  results: RiskResult[];
  nextCursor?: string;
  totalCount: number;
};

const FIRST_PAGE: Page = {
  results: [
    RULE_ROW,
    ORPHAN_RULE_ROW,
    MANUAL_ROW,
    AUTOMATED_ROW,
    SHADOW_MCP_ROW,
    LEGACY_SHADOW_ROW,
    ...FILLER_ROWS.slice(2),
  ],
  nextCursor: "page-2",
  totalCount: 14,
};

const SECOND_PAGE: Page = {
  results: FILLER_ROWS.slice(0, 4).map((row) =>
    makeResult({ ...row, id: `${row.id}-page-2` }),
  ),
  totalCount: 14,
};

// Mutable so a test can stage a different response; afterEach puts the
// defaults back, so a failing assertion can't leak its fixture into the tests
// that follow.
let PAGES: Record<string, Page | undefined> = {};

function resetPages() {
  PAGES = { "": FIRST_PAGE, "page-2": SECOND_PAGE };
}
resetPages();

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

afterEach(() => {
  cleanup();
  resetPages();
});

beforeEach(() => {
  restore.mockClear();
  openChat.mockClear();
  refetch.mockClear();
  optimisticallyRestored.clear();
  listState.isPlaceholderData = false;
  listState.isFetching = false;
  listState.isError = false;
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
    // afterEach restores the default fixture, so this stays local to the test
    // even if an assertion below throws.
    PAGES[""] = { results: [], totalCount: 0 };

    const { container } = renderSection();

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

  it("hides a row the hook is optimistically treating as restored", () => {
    // The suppressed listing is served from a mirror that lags the write, so
    // a refetch can still carry the row that was just restored.
    optimisticallyRestored.add("manual-row");
    renderSection();
    expand();

    expect(
      screen.queryByRole("button", {
        name: `View suppressed finding ${getRuleTitleFallback("generic-api-key")}`,
      }),
    ).toBeNull();
    expect(screen.getAllByRole("button", { name: "Restore" })).toHaveLength(7);
  });

  it("discounts optimistically hidden rows from the total and the range", () => {
    optimisticallyRestored.add("manual-row");
    optimisticallyRestored.add("automated-row");
    renderSection();
    expand();

    // 14 suppressed server-side, two of them hidden here pending the mirror.
    expect(screen.getByText("Suppressed · 12")).toBeTruthy();
    expect(screen.getByText("1–8 of 12 suppressed")).toBeTruthy();
  });

  it("omits the evidence section when there is no match behind the fingerprint", () => {
    renderSection();
    expand();
    openRow("private-key");

    const panel = drawer();
    expect(panel.queryByText("Match")).toBeNull();
    expect(panel.queryByText("<redacted len=0>")).toBeNull();
    // The rest of the finding still renders.
    expect(panel.getByText("Suppressed automatically")).toBeTruthy();
  });

  it("does not claim a shadow MCP identifier is hidden when it is on screen", () => {
    renderSection();
    expand();
    openRow("unapproved-server");

    const panel = drawer();
    expect(panel.getByText("https://mcp.example.com/sse")).toBeTruthy();
    // The identifier is fully visible, so any "value stays hidden" line would
    // contradict what the reader is looking at.
    expect(panel.queryByText(/can't be revealed/i)).toBeNull();
    expect(panel.queryByText(/stays hidden/i)).toBeNull();
    expect(panel.getByText("Server")).toBeTruthy();
  });

  it("treats a legacy shadow MCP fingerprint as redacted, not as an identifier", () => {
    renderSection();
    expand();
    openRow("legacy-server");

    const panel = drawer();
    // Same source as the row above, but this value is a fingerprint — calling
    // it a server and dropping the guidance would misdescribe it.
    expect(panel.getByText("Match")).toBeTruthy();
    expect(panel.queryByText("Server")).toBeNull();
    expect(panel.getByText(/can't be revealed/i)).toBeTruthy();
  });

  it("points a rule-suppressed finding at its rule, not at a restore it cannot offer", () => {
    renderSection();
    expand();
    openRow("aws-access-token");

    const panel = drawer();
    expect(
      panel.getByText(/exclusion rule suppresses this finding/i),
    ).toBeTruthy();
    // "Restore it first" is impossible here — the drawer offers View rule.
    expect(panel.queryByText(/restore it first/i)).toBeNull();
  });

  it("keeps the discounted total steady when paging away from a hidden row", () => {
    optimisticallyRestored.add("manual-row");
    renderSection();
    expand();
    expect(screen.getByText("Suppressed · 13")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Next" }));

    // The hidden row lives on the page we just left, but the count must not
    // bounce back up — a total that climbs reads as the restore having failed.
    expect(screen.getByText("Suppressed · 13")).toBeTruthy();
  });

  it("locks the rows down while they belong to a page the pager has left", () => {
    // keepPreviousData holds the old page on screen while the requested one
    // loads, so acting on a row here would act on the wrong page's finding.
    listState.isPlaceholderData = true;
    renderSection();
    expand();

    expect(
      screen
        .getAllByRole("button", { name: "Restore" })[0]!
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(
      screen
        .getAllByLabelText("Select suppressed finding", {
          selector: "[role=checkbox]",
        })[0]!
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(
      screen.getByRole("button", { name: "Next" }).hasAttribute("disabled"),
    ).toBe(true);
    // pageIndex has moved on but the rows have not, so naming a range would
    // describe neither page.
    expect(screen.getByText("Loading…")).toBeTruthy();

    openRow("generic-api-key");
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("offers a retry instead of looking empty when the listing fails", () => {
    listState.isError = true;
    renderSection();

    expect(screen.getByText("Couldn't load suppressed findings.")).toBeTruthy();
    // The section must not silently vanish, which would read as "nothing is
    // suppressed".
    expect(screen.queryByText("Suppressed · 14")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(refetch).toHaveBeenCalled();
  });

  it("ignores Next while a page request is still in flight", () => {
    // A fetch that isn't serving placeholder data leaves Next clickable, but
    // the cursor on screen belongs to the request already outstanding —
    // advancing again would push it onto the stack a second time.
    listState.isFetching = true;
    renderSection();
    expand();

    fireEvent.click(screen.getByRole("button", { name: "Next" }));

    expect(screen.getByText("1–10 of 14 suppressed")).toBeTruthy();
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
