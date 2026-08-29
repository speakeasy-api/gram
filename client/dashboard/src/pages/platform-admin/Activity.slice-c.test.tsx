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
  return { ...actual, useInfiniteQuery: () => queryState };
});
import { OrganizationActivity } from "./Activity";

const baseLog = {
  acting_surface: "dashboard",
  actor_id: "<ACTOR_ID>",
  actor_type: "user",
  created_at: "2026-08-25T12:00:00Z",
  id: "event",
  subject_id: "<ORG_ID>",
  subject_type: "organization",
};
function setQuery(
  pages: Array<{ logs: Array<Record<string, unknown>> }>,
  extra = {},
) {
  queryState = {
    data: { pages },
    error: null,
    fetchNextPage,
    hasNextPage: false,
    isError: false,
    isFetchNextPageError: false,
    isFetchingNextPage: false,
    isLoading: false,
    isRefetchError: false,
    isRefetching: false,
    refetch,
    ...extra,
  };
}

describe("organization activity slice C", () => {
  afterEach(cleanup);
  beforeEach(() => {
    fetchNextPage.mockReset();
    refetch.mockReset();
    setQuery([{ logs: [] }]);
  });

  it("shows every event in fetched order with first-seen dedupe and only specializes five trial actions", () => {
    const armed = {
      ...baseLog,
      id: "armed",
      action: "organization:enterprise_trial_armed",
      actor_display_name: "  Signup User  ",
    };
    setQuery([
      {
        logs: [
          armed,
          { ...baseLog, id: "other", action: "project:update" },
          {
            ...baseLog,
            id: "future",
            action: "organization:enterprise_trial_paused",
          },
        ],
      },
      {
        logs: [
          armed,
          {
            ...baseLog,
            id: "converted",
            action: "organization:enterprise_trial_converted",
            created_at: "2027-01-01T00:00:00Z",
            metadata: { conversion_source: "stripe_checkout" },
          },
        ],
      },
    ]);
    render(<OrganizationActivity organizationId="<ORG_ID>" />);
    expect(
      screen.getAllByTestId(/^activity-/).map((item) => item.dataset.testid),
    ).toEqual([
      "activity-armed",
      "activity-other",
      "activity-future",
      "activity-converted",
    ]);
    expect(
      within(screen.getByTestId("activity-armed")).getByText(
        "started enterprise trial",
      ),
    ).toBeDefined();
    expect(
      within(screen.getByTestId("activity-armed")).getAllByText(
        "Signup User",
      )[0],
    ).toBeDefined();
    const other = screen.getByTestId("activity-other");
    const future = screen.getByTestId("activity-future");
    expect(within(other).getByText("updated organization")).toBeDefined();
    expect(within(future).getByText("updated organization")).toBeDefined();
    fireEvent.click(within(other).getByText("Details"));
    expect(within(other).getByText("project:update")).toBeDefined();
    expect(
      within(screen.getByTestId("activity-converted")).getAllByText(
        "Stripe",
      )[0],
    ).toBeDefined();
  });

  it("uses approved actor fallbacks, UTC timestamps, and opens the armed event details", () => {
    setQuery([
      {
        logs: [
          {
            ...baseLog,
            id: "armed",
            action: "organization:enterprise_trial_armed",
            actor_id: "legacy-signup",
          },
          {
            ...baseLog,
            id: "extended",
            action: "organization:enterprise_trial_extended",
            actor_id: "legacy-staff",
          },
          {
            ...baseLog,
            id: "manual",
            action: "organization:enterprise_trial_converted",
            actor_display_name: "Manual Admin",
            metadata: { conversion_source: "platform_admin" },
          },
          {
            ...baseLog,
            id: "system",
            action: "organization:enterprise_trial_demoted",
            actor_id: "system",
          },
        ],
      },
    ]);
    render(<OrganizationActivity organizationId="<ORG_ID>" />);
    expect(
      within(screen.getByTestId("activity-armed")).getAllByText(
        "Unknown user",
      )[0],
    ).toBeDefined();
    expect(
      within(screen.getByTestId("activity-extended")).getAllByText(
        "Speakeasy Team",
      )[0],
    ).toBeDefined();
    expect(
      within(screen.getByTestId("activity-manual")).getAllByText(
        "Manual Admin",
      )[0],
    ).toBeDefined();
    expect(
      within(screen.getByTestId("activity-system")).getAllByText("System")[0],
    ).toBeDefined();
    const armedArticle = screen.getByTestId("activity-armed");
    expect(armedArticle.querySelector("time")?.textContent).toBe(
      "Tuesday, August 25, 2026 at 12:00:00 PM UTC",
    );
    expect(armedArticle.querySelector("time")?.getAttribute("datetime")).toBe(
      "2026-08-25T12:00:00Z",
    );
    const armedDetails = within(armedArticle)
      .getByText("Details")
      .closest("details");
    const extendedDetails = within(screen.getByTestId("activity-extended"))
      .getByText("Details")
      .closest("details");
    fireEvent.click(within(armedArticle).getByText("Details"));
    expect(armedDetails?.open).toBe(true);
    expect(
      within(armedArticle).getByText("organization:enterprise_trial_armed"),
    ).toBeDefined();
    expect(extendedDetails?.open).toBe(false);
  });

  it("renders compact allowlisted trial facts and records missing legacy values", () => {
    setQuery([
      {
        logs: [
          {
            ...baseLog,
            id: "armed",
            action: "organization:enterprise_trial_armed",
            after_snapshot: {
              trial: { tier: "enterprise", ends_at: "2026-09-08T12:00:00Z" },
              keys: [{ key_type: "chat", effective_disabled: false }],
            },
          },
          {
            ...baseLog,
            id: "extended",
            action: "organization:enterprise_trial_extended",
            before_snapshot: { trial: { ends_at: "2026-09-08T12:00:00Z" } },
            after_snapshot: { trial: { ends_at: "2026-09-22T12:00:00Z" } },
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
            before_snapshot: {
              keys: [{ key_type: "chat", effective_disabled: false }],
            },
            after_snapshot: {
              trial: { demoted_at: "2026-08-25T12:00:00Z" },
              keys: [{ key_type: "chat", effective_disabled: true }],
            },
          },
          {
            ...baseLog,
            id: "converted",
            action: "organization:enterprise_trial_converted",
            metadata: { conversion_source: "stripe_checkout" },
            after_snapshot: { trial: { converted_at: "2026-08-25T12:00:00Z" } },
          },
        ],
      },
    ]);
    render(<OrganizationActivity organizationId="<ORG_ID>" />);
    const armed = screen.getByTestId("activity-armed");
    expect(within(armed).getByText("Enterprise trial started")).toBeDefined();
    expect(armed.textContent).toContain("Tierenterprise");
    expect(armed.textContent).toContain("Trial end");
    expect(armed.textContent).toContain("Key access (chat)Enabled");
    expect(screen.getByTestId("activity-extended").textContent).toContain(
      "Previous trial end",
    );
    expect(screen.getByTestId("activity-rearmed").textContent).toContain(
      "Not recorded",
    );
    expect(screen.getByTestId("activity-demoted").textContent).toContain(
      "Key access (chat)Enabled → Disabled",
    );
    expect(screen.getByTestId("activity-converted").textContent).toContain(
      "Conversion methodStripe checkout",
    );
  });

  it("exposes only privacy-filtered details and sanitized structured/raw diffs", () => {
    setQuery([
      {
        logs: [
          {
            ...baseLog,
            id: "private",
            action: "organization:update",
            actor_display_name: "Safe actor",
            actor_id: "identity-secret",
            actor_slug: "private-actor-slug",
            subject_display_name: "Safe organization",
            subject_id: "organization-secret",
            acting_client_id: "client-secret",
            metadata: {
              conversion_source: "platform_admin",
              tier: "enterprise",
              prompt: "private prompt",
              spend: 1234,
              provider_payload: { token: "provider-secret" },
              identity_id: "identity-secret",
            },
            before_snapshot: {
              organization: {
                account_type: "",
                whitelisted: null,
                disabled: false,
                api_key: "sk-secret",
              },
              keys: [
                {
                  key_type: "chat",
                  disable_causes: [],
                  effective_disabled: false,
                  hash: "hash-secret",
                },
              ],
            },
            after_snapshot: {
              organization: {
                account_type: "enterprise",
                whitelisted: true,
                disabled: false,
              },
              keys: [
                {
                  key_type: "chat",
                  disable_causes: ["trial_demotion"],
                  effective_disabled: true,
                  monthly_credits: 50,
                },
              ],
            },
          },
        ],
      },
    ]);
    render(<OrganizationActivity organizationId="<ORG_ID>" />);
    const item = screen.getByTestId("activity-private");
    fireEvent.click(within(item).getByText("Details"));
    for (const visible of [
      "Safe actor",
      "Safe organization",
      "dashboard",
      "platform_admin",
      "enterprise",
      '""',
      "null",
      "[]",
      "false",
      "(missing) → 50",
    ])
      expect(item.textContent).toContain(visible);
    for (const secret of [
      "identity-secret",
      "organization-secret",
      "client-secret",
      "private-actor-slug",
      "private prompt",
      "1234",
      "provider-secret",
      "sk-secret",
      "hash-secret",
    ])
      expect(item.textContent).not.toContain(secret);
    fireEvent.click(
      within(item).getByRole("button", { name: "View raw diff" }),
    );
    expect(
      within(item).getByRole("region", { name: "Raw diff" }),
    ).toBeDefined();
    expect(item.textContent).toContain("account_type");
    expect(item.textContent).not.toContain("sk-secret");
    expect(item.textContent).not.toContain("hash-secret");
  });

  it("keeps cached empty or nonempty data visible for refetch errors and retries with refetch", () => {
    setQuery([{ logs: [] }], { isError: true, isRefetchError: true });
    const { rerender } = render(
      <OrganizationActivity organizationId="<ORG_ID>" />,
    );
    expect(screen.getByText("No activity yet.")).toBeDefined();
    expect(screen.getByRole("alert").textContent).toContain(
      "Activity could not be refreshed.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry refresh" }));
    expect(refetch).toHaveBeenCalledTimes(1);
    setQuery(
      [{ logs: [{ ...baseLog, id: "cached", action: "project:update" }] }],
      { isError: true, isRefetchError: true },
    );
    rerender(<OrganizationActivity organizationId="<ORG_ID>" />);
    expect(screen.getByTestId("activity-cached")).toBeDefined();
    expect(screen.getByRole("alert").textContent).toContain(
      "Activity could not be refreshed.",
    );
  });
});
