import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const fetchNextPage = vi.fn();
const refetch = vi.fn();
let queryState: Record<string, unknown>;

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useInfiniteQuery: () => queryState,
  };
});

import { OrganizationActivity } from "./Activity";

const baseLog = {
  acting_surface: "unknown",
  actor_id: "system",
  actor_type: "user",
  created_at: "2026-08-25T12:00:00Z",
  id: "log-base",
  subject_id: "org-current",
  subject_type: "organization",
};

function ready(logs: Array<Record<string, unknown>>, extra = {}) {
  queryState = {
    data: { pages: [{ logs }] },
    error: null,
    fetchNextPage,
    hasNextPage: false,
    isError: false,
    isFetchingNextPage: false,
    isLoading: false,
    refetch,
    ...extra,
  };
}

describe("organization trial activity", () => {
  afterEach(cleanup);

  beforeEach(() => {
    fetchNextPage.mockReset();
    refetch.mockReset();
    ready([]);
  });

  it("humanizes all five actions, conversion sources, and stable actor fallback", () => {
    ready([
      {
        ...baseLog,
        id: "armed",
        action: "organization:enterprise_trial_armed",
      },
      {
        ...baseLog,
        id: "extended",
        action: "organization:enterprise_trial_extended",
      },
      {
        ...baseLog,
        id: "rearmed",
        action: "organization:enterprise_trial_rearmed",
      },
      {
        ...baseLog,
        id: "demoted",
        action: "organization:enterprise_trial_demoted",
      },
      {
        ...baseLog,
        id: "manual",
        action: "organization:enterprise_trial_converted",
        actor_id: "staff",
        actor_display_name: "Platform administrator",
        metadata: { conversion_source: "platform_admin" },
      },
      {
        ...baseLog,
        id: "checkout",
        action: "organization:enterprise_trial_converted",
        metadata: { conversion_source: "stripe_checkout" },
      },
    ]);

    render(<OrganizationActivity organizationId="org-current" />);

    expect(screen.getByText("started enterprise trial")).toBeDefined();
    expect(screen.getByText("extended enterprise trial")).toBeDefined();
    expect(screen.getByText("rearmed enterprise trial")).toBeDefined();
    expect(screen.getByText("demoted enterprise trial")).toBeDefined();
    expect(screen.getByText("marked enterprise trial converted")).toBeDefined();
    expect(
      screen.getByText("converted enterprise trial through checkout"),
    ).toBeDefined();
    expect(screen.getAllByText("Platform administrator")[0]).toBeDefined();
    expect(screen.getAllByText("System").length).toBeGreaterThan(0);

    const armedArticle = screen.getByTestId("activity-armed");
    const armedDetails = within(armedArticle)
      .getByText("Details")
      .closest("details");
    fireEvent.click(within(armedArticle).getByText("Details"));
    expect(armedDetails?.open).toBe(true);
    expect(
      within(armedArticle).getByText("organization:enterprise_trial_armed"),
    ).toBeDefined();
  });

  it("renders only structured allowlisted before/after diffs", () => {
    ready([
      {
        ...baseLog,
        id: "converted",
        action: "organization:enterprise_trial_converted",
        metadata: {
          conversion_source: "stripe_checkout",
          prompt: "never render me",
        },
        before_snapshot: {
          organization: {
            account_type: "free",
            whitelisted: false,
            disabled: false,
            email: "private@example.test",
          },
          trial: {
            tier: "enterprise",
            ends_at: "2026-09-01T00:00:00Z",
            converted_at: null,
            demoted_at: null,
          },
          keys: [
            {
              key_type: "chat",
              disable_causes: ["trial_demotion"],
              stored_disabled: true,
              effective_disabled: true,
              monthly_credits: 7,
              key_hash: "hash-secret",
            },
          ],
          provider_payload: { key: "sk-secret" },
        },
        after_snapshot: {
          organization: {
            account_type: "enterprise",
            whitelisted: true,
            disabled: false,
          },
          trial: {
            tier: "enterprise",
            ends_at: "2026-09-01T00:00:00Z",
            converted_at: "2026-08-25T12:00:00Z",
            demoted_at: null,
          },
          keys: [
            {
              key_type: "chat",
              disable_causes: [],
              stored_disabled: false,
              effective_disabled: false,
              monthly_credits: 50,
            },
          ],
        },
      },
    ]);

    render(<OrganizationActivity organizationId="org-current" />);
    const item = screen.getByTestId("activity-converted");
    expect(within(item).getByText("Account type")).toBeDefined();
    expect(within(item).getByText('"free" → "enterprise"')).toBeDefined();
    expect(within(item).getByText("Monthly cap (chat)")).toBeDefined();
    expect(within(item).getByText("7 → 50")).toBeDefined();
    expect(within(item).getByText('"free" → "enterprise"').className).toContain(
      "break-all",
    );
    expect(item.textContent).not.toContain("private@example.test");
    expect(item.textContent).not.toContain("hash-secret");
    expect(item.textContent).not.toContain("sk-secret");
    expect(item.textContent).not.toContain("never render me");
    expect(item.textContent).not.toContain("provider_payload");
  });

  it("preserves fetched order, deduplicates page boundaries, and fetches each next page once", () => {
    const older = {
      ...baseLog,
      id: "older",
      action: "organization:enterprise_trial_armed",
      created_at: "2026-08-24T12:00:00Z",
    };
    const newer = {
      ...baseLog,
      id: "newer",
      action: "organization:enterprise_trial_demoted",
      created_at: "2026-08-26T12:00:00Z",
    };
    queryState = {
      data: { pages: [{ logs: [older, newer] }, { logs: [older] }] },
      error: null,
      fetchNextPage,
      hasNextPage: true,
      isError: false,
      isFetchingNextPage: false,
      isLoading: false,
      refetch,
    };

    render(<OrganizationActivity organizationId="org-current" />);
    const items = screen.getAllByTestId(/^activity-/);
    expect(items.map((item) => item.dataset.testid)).toEqual([
      "activity-older",
      "activity-newer",
    ]);
    fireEvent.click(
      screen.getByRole("button", { name: "Load older activity" }),
    );
    expect(fetchNextPage).toHaveBeenCalledTimes(1);
  });

  it("announces incremental loading", () => {
    ready(
      [
        {
          ...baseLog,
          id: "armed",
          action: "organization:enterprise_trial_armed",
        },
      ],
      { hasNextPage: true, isFetchingNextPage: true },
    );
    render(<OrganizationActivity organizationId="org-current" />);
    expect(screen.getByRole("status").textContent).toBe(
      "Loading older activity…",
    );
  });

  it("shows loading, empty, and retryable error states", () => {
    queryState = { isLoading: true };
    const { rerender } = render(
      <OrganizationActivity organizationId="org-current" />,
    );
    expect(screen.getByRole("status").textContent).toBe("Loading activity…");

    queryState = {
      isLoading: false,
      isError: true,
      error: new Error("private failure"),
      refetch,
    };
    rerender(<OrganizationActivity organizationId="org-current-error" />);
    expect(screen.getByRole("alert").textContent).toContain(
      "Activity could not be loaded.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(refetch).toHaveBeenCalledTimes(1);

    ready([]);
    rerender(<OrganizationActivity organizationId="org-current-empty" />);
    expect(screen.getByText("No activity yet.")).toBeDefined();
  });
});
