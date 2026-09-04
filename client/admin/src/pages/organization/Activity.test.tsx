import { useState } from "react";
import { infiniteQueryOptions, QueryClient } from "@tanstack/react-query";
import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { organizationActivityQuery } from "@/lib/gramAdminClient";
import { Activity } from "@/pages/organization/Activity";
import { anActivityLog, anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

const mocks = vi.hoisted(() => ({ listOrganizationActivity: vi.fn() }));

vi.mock("@/lib/gramAdminClient", () => ({
  organizationActivityQuery: (organizationId: string) =>
    infiniteQueryOptions({
      queryKey: ["activity", organizationId],
      initialPageParam: undefined as { cursor: string } | undefined,
      queryFn: async ({ pageParam }) => {
        const result = await mocks.listOrganizationActivity(
          organizationId,
          pageParam?.cursor,
        );
        return {
          result,
          "~next": result.nextCursor
            ? { cursor: result.nextCursor }
            : undefined,
        };
      },
      getNextPageParam: (page) => page["~next"],
    }),
}));

const ORG = anOrganization();

function deferred<T>(): {
  promise: Promise<T>;
  resolve: (value: T) => void;
  reject: (reason: unknown) => void;
} {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((settle, fail) => {
    resolve = settle;
    reject = fail;
  });
  return { promise, resolve, reject };
}

beforeEach(() => mocks.listOrganizationActivity.mockReset());
afterEach(cleanup);

describe("Activity", () => {
  it("renders every event field and the display, slug, and ID fallbacks", async () => {
    const createdAt = new Date("2026-01-15T12:30:00Z");
    mocks.listOrganizationActivity.mockResolvedValue({
      logs: [
        anActivityLog({
          id: "event-display",
          createdAt: createdAt,
          actorId: "actor-display-id",
          actorType: "staff",
          actorDisplayName: "Actor Display",
          actorSlug: "actor-display-slug",
          subjectId: "subject-display-id",
          subjectType: "organization",
          subjectDisplayName: "Subject Display",
          subjectSlug: "subject-display-slug",
          projectId: "project-id",
          projectSlug: "project-slug",
          actingSurface: "platform_mcp",
          actingClientId: "client-id",
          beforeSnapshot: { days: 7 },
          afterSnapshot: { days: 14 },
          metadata: { approved: true },
        }),
        anActivityLog({
          id: "event-slug",
          actorId: "actor-slug-id",
          actorDisplayName: undefined,
          actorSlug: "actor-slug",
          subjectId: "subject-slug-id",
          subjectDisplayName: undefined,
          subjectSlug: "subject-slug",
        }),
        anActivityLog({
          id: "event-id",
          actorId: "actor-id-fallback",
          actorDisplayName: undefined,
          actorSlug: undefined,
          subjectId: "subject-id-fallback",
          subjectDisplayName: undefined,
          subjectSlug: undefined,
        }),
      ],
    });

    await renderWithApp(<Activity org={ORG} />);

    const list = await screen.findByRole("list");
    expect(mocks.listOrganizationActivity).toHaveBeenCalledWith(
      ORG.id,
      undefined,
    );
    const firstSummary = list.querySelectorAll("li > div")[0]?.textContent;
    expect(firstSummary).toContain("Actor Display");
    expect(firstSummary).not.toContain("actor-display-slug");
    expect(firstSummary).toContain("Subject Display");
    expect(firstSummary).not.toContain("subject-display-slug");
    expect(list.textContent).toContain("actor-slug");
    expect(list.textContent).toContain("subject-slug");
    expect(list.textContent).toContain("actor-id-fallback");
    expect(list.textContent).toContain("subject-id-fallback");
    expect(list.textContent).toContain("via platform_mcp");

    const time = list.querySelector("time");
    expect(time?.getAttribute("datetime")).toBe(createdAt.toISOString());
    expect(time?.textContent).toBe(
      "Thursday, January 15, 2026 at 12:30:00 PM UTC",
    );

    const details = screen.getAllByText(/Event details for/)[0];
    if (!details) throw new Error("missing event details");
    fireEvent.click(details);
    for (const value of [
      "project-id",
      "project-slug",
      "staff",
      "actor-display-id",
      "actor-display-slug",
      "organization",
      "subject-display-id",
      "subject-display-slug",
      "client-id",
    ]) {
      expect(screen.queryAllByText(value).length).toBeGreaterThan(0);
    }
    expect(screen.getByText(/"approved": true/)).toBeTruthy();
  });

  it("toggles a structured diff from the activity summary row", async () => {
    mocks.listOrganizationActivity.mockResolvedValue({
      logs: [
        anActivityLog({
          beforeSnapshot: {
            zeta: "before",
            removed: "gone",
            complex: { nested: ["old"] },
            alpha: null,
          },
          afterSnapshot: {
            zeta: "after",
            added: true,
            complex: { nested: ["new"] },
            alpha: false,
          },
        }),
      ],
    });

    await renderWithApp(<Activity org={ORG} />);

    const showDiff = await screen.findByRole("button", { name: "Show diff ▾" });
    const summary = showDiff.closest("li > div");
    expect(summary).toBeTruthy();
    expect(showDiff.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByText("Changed fields")).toBeNull();

    fireEvent.click(showDiff);

    const hideDiff = screen.getByRole("button", { name: "Hide diff ▴" });
    expect(hideDiff.getAttribute("aria-expanded")).toBe("true");
    expect(summary?.nextElementSibling?.getAttribute("id")).toBe(
      showDiff.getAttribute("aria-controls"),
    );
    expect(screen.getByText("Changed fields").className).toContain("uppercase");
    expect(screen.getByText("5 fields changed")).toBeTruthy();

    const changedFields = screen.getByRole("region", {
      name: "Changed fields",
    });
    const rows = within(changedFields).getAllByRole("listitem");
    expect(rows.map((row) => row.getAttribute("data-field"))).toEqual([
      "added",
      "alpha",
      "complex",
      "removed",
      "zeta",
    ]);
    expect(within(rows[4]!).getByText("zeta").className).toContain("w-[140px]");
    expect(within(rows[4]!).getByText('"before"').className).toContain(
      "line-through",
    );
    expect(within(rows[4]!).getByText('"before"').className).toContain(
      "bg-red-50",
    );
    expect(within(rows[4]!).getByText("→")).toBeTruthy();
    expect(within(rows[4]!).getByText('"after"').className).toContain(
      "bg-emerald-50",
    );
    expect(rows[0]?.textContent).toBe("added(none)→true");
    expect(rows[1]?.textContent).toBe("alphanull→false");
    expect(rows[2]?.textContent).toBe(
      'complex{"nested":["old"]}→{"nested":["new"]}',
    );
    expect(rows[3]?.textContent).toBe('removed"gone"→(none)');
  });

  it("uses the legacy system actor ID fallback", async () => {
    mocks.listOrganizationActivity.mockResolvedValue({
      logs: [
        anActivityLog({
          actorId: "system",
          actorType: "staff",
          actorDisplayName: undefined,
          actorSlug: undefined,
        }),
      ],
    });

    await renderWithApp(<Activity org={ORG} />);
    expect((await screen.findByRole("list")).textContent).toContain("System");
  });

  it("renders an empty string as a visible JSON string in a changed field", async () => {
    mocks.listOrganizationActivity.mockResolvedValue({
      logs: [
        anActivityLog({
          beforeSnapshot: { value: "before" },
          afterSnapshot: { value: "" },
        }),
      ],
    });

    await renderWithApp(<Activity org={ORG} />);
    fireEvent.click(await screen.findByRole("button", { name: "Show diff ▾" }));

    const changedField = within(
      screen.getByRole("region", { name: "Changed fields" }),
    ).getByRole("listitem");
    expect(changedField.textContent).toBe('value"before"→""');
  });

  it("distinguishes a primitive from its string representation", async () => {
    mocks.listOrganizationActivity.mockResolvedValue({
      logs: [
        anActivityLog({
          beforeSnapshot: { value: true },
          afterSnapshot: { value: "true" },
        }),
      ],
    });

    await renderWithApp(<Activity org={ORG} />);
    fireEvent.click(await screen.findByRole("button", { name: "Show diff ▾" }));

    const changedField = within(
      screen.getByRole("region", { name: "Changed fields" }),
    ).getByRole("listitem");
    expect(changedField.textContent).toBe('valuetrue→"true"');
  });

  it("switches between structured and raw before/after snapshots", async () => {
    mocks.listOrganizationActivity.mockResolvedValue({
      logs: [
        anActivityLog({
          beforeSnapshot: { days: 7 },
          afterSnapshot: { days: 14 },
        }),
      ],
    });
    await renderWithApp(<Activity org={ORG} />);
    fireEvent.click(await screen.findByRole("button", { name: "Show diff ▾" }));
    fireEvent.click(screen.getByRole("button", { name: "View raw diff" }));

    expect(screen.getByText("Before snapshot")).toBeTruthy();
    expect(screen.getByText(/"days": 7/)).toBeTruthy();
    expect(screen.getByText("After snapshot")).toBeTruthy();
    expect(screen.getByText(/"days": 14/)).toBeTruthy();
    expect(screen.queryByText("Changed fields")).toBeNull();

    fireEvent.click(
      screen.getByRole("button", { name: "View structured diff" }),
    );
    expect(screen.getByText("Changed fields")).toBeTruthy();
    expect(screen.queryByText("Before snapshot")).toBeNull();
  });

  it("omits diff UI without changes and preserves Event details metadata", async () => {
    const snapshot = { unchanged: "same" };
    mocks.listOrganizationActivity.mockResolvedValue({
      logs: [
        anActivityLog({
          beforeSnapshot: snapshot,
          afterSnapshot: snapshot,
          metadata: { request_id: "request-placeholder" },
        }),
      ],
    });

    await renderWithApp(<Activity org={ORG} />);
    expect(screen.queryByRole("button", { name: /diff/ })).toBeNull();
    expect(screen.queryByText("Changed fields")).toBeNull();

    fireEvent.click(
      await screen.findByText(
        "Event details for organization:settings_updated",
      ),
    );
    expect(screen.getByText("Actor ID")).toBeTruthy();
    expect(screen.getByText("Subject ID")).toBeTruthy();
    expect(screen.getByText("Metadata")).toBeTruthy();
    expect(
      screen.getByText(/"request_id": "request-placeholder"/),
    ).toBeTruthy();
    expect(screen.queryByText("Raw snapshots")).toBeNull();
  });

  it("shows only the initial loading state while the first page is pending", async () => {
    const firstPage = deferred<{ logs: ReturnType<typeof anActivityLog>[] }>();
    mocks.listOrganizationActivity.mockReturnValueOnce(firstPage.promise);

    await renderWithApp(<Activity org={ORG} />);

    expect(screen.getByText("Loading activity...")).toBeTruthy();
    expect(screen.queryByRole("list")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByText("No activity for this organization")).toBeNull();
    firstPage.resolve({ logs: [] });
    await screen.findByText("No activity for this organization");
  });

  it("shows only the empty state after an empty first page", async () => {
    mocks.listOrganizationActivity.mockResolvedValue({ logs: [] });

    await renderWithApp(<Activity org={ORG} />);

    expect(
      await screen.findByText("No activity for this organization"),
    ).toBeTruthy();
    expect(screen.queryByText("Loading activity...")).toBeNull();
    expect(screen.queryByRole("list")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("does not claim the feed is empty while another page is available", async () => {
    mocks.listOrganizationActivity.mockResolvedValue({
      logs: [],
      nextCursor: "next-page",
    });

    await renderWithApp(<Activity org={ORG} />);
    await screen.findByRole("button", { name: "Load more" });
    expect(screen.queryByText("No activity for this organization")).toBeNull();
  });

  it("keeps the empty state and shows only the refresh error after a failed refetch", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    mocks.listOrganizationActivity
      .mockResolvedValueOnce({ logs: [] })
      .mockRejectedValueOnce(new Error("offline"));
    await renderWithApp(<Activity org={ORG} />, { queryClient });
    await screen.findByText("No activity for this organization");

    await queryClient
      .invalidateQueries({
        queryKey: organizationActivityQuery(ORG.id).queryKey,
      })
      .catch(() => undefined);

    expect(await screen.findByText("Unable to refresh activity")).toBeTruthy();
    expect(screen.getByText("No activity for this organization")).toBeTruthy();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(screen.queryByText(/^Unable to load activity$/)).toBeNull();
    expect(screen.getByRole("button", { name: "Retry refresh" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  });

  it("shows only an alert and retry control after an initial failure", async () => {
    mocks.listOrganizationActivity
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce({ logs: [] });

    await renderWithApp(<Activity org={ORG} />);

    expect((await screen.findByRole("alert")).textContent).toContain(
      "Unable to load activity",
    );
    expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
    expect(screen.queryByText("Loading activity...")).toBeNull();
    expect(screen.queryByText("No activity for this organization")).toBeNull();
    expect(screen.queryByText("Unable to load more activity")).toBeNull();
    expect(screen.queryByRole("list")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await screen.findByText("No activity for this organization");
  });

  it("retries an initial failure and replaces it with the empty state", async () => {
    mocks.listOrganizationActivity
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce({ logs: [] });
    await renderWithApp(<Activity org={ORG} />);

    fireEvent.click(await screen.findByRole("button", { name: "Retry" }));

    expect(
      await screen.findByText("No activity for this organization"),
    ).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(mocks.listOrganizationActivity).toHaveBeenCalledTimes(2);
  });

  it("keeps rows and prevents duplicate retries after a refresh failure", async () => {
    const retry = deferred<{ logs: ReturnType<typeof anActivityLog>[] }>();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    mocks.listOrganizationActivity
      .mockResolvedValueOnce({
        logs: [anActivityLog({ id: "event-1", action: "event-1" })],
      })
      .mockRejectedValueOnce(new Error("offline"))
      .mockReturnValueOnce(retry.promise);
    await renderWithApp(<Activity org={ORG} />, { queryClient });
    await screen.findByText("event-1");

    await queryClient
      .refetchQueries({
        queryKey: organizationActivityQuery(ORG.id).queryKey,
        type: "active",
      })
      .catch(() => undefined);

    expect((await screen.findByRole("alert")).textContent).toContain(
      "Unable to refresh activity",
    );
    expect(screen.getByText("event-1")).toBeTruthy();
    expect(screen.queryByText("Unable to load more activity")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry refresh" }));

    const retrying = await screen.findByRole("button", {
      name: "Retrying refresh...",
    });
    expect(retrying.hasAttribute("disabled")).toBe(true);
    fireEvent.click(retrying);
    expect(mocks.listOrganizationActivity).toHaveBeenCalledTimes(3);

    retry.resolve({
      logs: [anActivityLog({ id: "event-2", action: "event-2" })],
    });
    await screen.findByText("event-2");
    expect(screen.queryByText("event-1")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("appends two rows per page in exact order, exactly once", async () => {
    const secondPage = deferred<{ logs: ReturnType<typeof anActivityLog>[] }>();
    mocks.listOrganizationActivity
      .mockResolvedValueOnce({
        logs: [
          anActivityLog({ id: "event-4", action: "event-4" }),
          anActivityLog({ id: "event-3", action: "event-3" }),
        ],
        nextCursor: "cursor-2",
      })
      .mockReturnValueOnce(secondPage.promise);
    await renderWithApp(<Activity org={ORG} />);

    fireEvent.click(await screen.findByRole("button", { name: "Load more" }));
    const loading = await screen.findByRole("button", { name: "Loading..." });
    expect(loading.hasAttribute("disabled")).toBe(true);
    fireEvent.click(loading);
    expect(mocks.listOrganizationActivity).toHaveBeenCalledTimes(2);
    expect(mocks.listOrganizationActivity).toHaveBeenLastCalledWith(
      ORG.id,
      "cursor-2",
    );

    secondPage.resolve({
      logs: [
        anActivityLog({ id: "event-2", action: "event-2" }),
        anActivityLog({ id: "event-1", action: "event-1" }),
      ],
    });
    await screen.findByText("event-1");

    const actions = Array.from(
      screen.getByRole("list").querySelectorAll("strong"),
      (node) => node.textContent,
    );
    expect(actions).toEqual(["event-4", "event-3", "event-2", "event-1"]);
    for (const action of actions) {
      expect(actions.filter((candidate) => candidate === action)).toHaveLength(
        1,
      );
    }
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
  });

  it("humanizes trial actions and interprets Stripe checkout conversion", async () => {
    mocks.listOrganizationActivity.mockResolvedValue({
      logs: [
        anActivityLog({
          id: "armed",
          action: "organization:enterprise_trial_armed",
          createdAt: new Date("2026-08-01T12:00:00Z"),
          afterSnapshot: {
            trial: { tier: "enterprise", ends_at: "2026-08-15T12:00:00Z" },
          },
        }),
        anActivityLog({
          id: "extended",
          action: "organization:enterprise_trial_extended",
          beforeSnapshot: { trial: { ends_at: "2026-08-15T12:00:00Z" } },
          afterSnapshot: {
            trial: { tier: "enterprise", ends_at: "2026-08-29T12:00:00Z" },
          },
        }),
        anActivityLog({
          id: "rearmed",
          action: "organization:enterprise_trial_rearmed",
        }),
        anActivityLog({
          id: "demoted",
          action: "organization:enterprise_trial_demoted",
          metadata: { previous_account_type: "enterprise" },
        }),
        anActivityLog({
          id: "converted",
          action: "organization:enterprise_trial_converted",
          metadata: {
            conversion_source: "stripe_checkout",
            key_access_changed: true,
          },
          beforeSnapshot: {
            keys: [{ key_type: "chat", effective_disabled: true }],
          },
          afterSnapshot: {
            trial: { converted_at: "2026-08-20T12:00:00Z", tier: "enterprise" },
            keys: [{ key_type: "chat", effective_disabled: false }],
          },
        }),
      ],
    });

    await renderWithApp(<Activity org={ORG} />);

    for (const phrase of [
      "started enterprise trial",
      "extended enterprise trial",
      "rearmed enterprise trial",
      "demoted enterprise trial",
      "converted enterprise trial through checkout",
    ]) {
      expect(await screen.findByText(phrase)).toBeTruthy();
    }
    const checkout = screen
      .getByText("converted enterprise trial through checkout")
      .closest("li");
    expect(checkout?.textContent).toContain("Stripe");
    expect(checkout?.textContent).toContain("Enterprise trial converted");
    expect(checkout?.textContent).toContain("Conversion methodStripe checkout");
    expect(checkout?.textContent).toContain("Tierenterprise");
    expect(checkout?.textContent).toContain("OpenRouter key access changedYes");
    expect(checkout?.textContent).toContain(
      "OpenRouter chat keyDisabled → Enabled",
    );
    expect(
      screen.getByRole("region", { name: "Enterprise trial started" })
        .textContent,
    ).toContain("Trial end");
    expect(
      screen.getByRole("region", { name: "Enterprise trial extended" })
        .textContent,
    ).toContain("Previous trial end");
    expect(
      screen.getByRole("region", { name: "Enterprise trial ended" })
        .textContent,
    ).toContain("Tierenterprise");
  });

  it.each([
    [
      "trial tier object",
      { trial: { tier: { name: "enterprise" } } },
      {},
      '{"name":"enterprise"}',
    ],
    [
      "trial tier array",
      { trial: { tier: ["enterprise"] } },
      {},
      '["enterprise"]',
    ],
    [
      "account type object",
      {},
      { account_type: { name: "enterprise" } },
      '{"name":"enterprise"}',
    ],
    [
      "account type array",
      {},
      { account_type: ["enterprise"] },
      '["enterprise"]',
    ],
    ["missing tier", {}, {}, "Not recorded"],
    ["null tier", { trial: { tier: null } }, {}, "Not recorded"],
    ["empty tier", { trial: { tier: "" } }, {}, "Not recorded"],
  ])(
    "renders %s in trial facts",
    async (_name, afterSnapshot, metadata, expected) => {
      mocks.listOrganizationActivity.mockResolvedValue({
        logs: [
          anActivityLog({
            action: "organization:enterprise_trial_armed",
            afterSnapshot,
            metadata,
          }),
        ],
      });

      await renderWithApp(<Activity org={ORG} />);
      const facts = await screen.findByRole("region", {
        name: "Enterprise trial started",
      });
      expect(
        within(facts).getByText("Tier").nextElementSibling?.textContent,
      ).toBe(expected);
    },
  );

  it("renders malformed trial dates without breaking the feed", async () => {
    mocks.listOrganizationActivity.mockResolvedValue({
      logs: [
        anActivityLog({
          action: "organization:enterprise_trial_rearmed",
          createdAt: new Date("not-a-date"),
        }),
      ],
    });

    await renderWithApp(<Activity org={ORG} />);
    expect(await screen.findByText("rearmed enterprise trial")).toBeTruthy();
    expect(screen.getAllByText("Unknown time").length).toBeGreaterThan(0);
  });

  it("adds labeled nested trial, organization, and key diffs without losing Admin details", async () => {
    mocks.listOrganizationActivity.mockResolvedValue({
      logs: [
        anActivityLog({
          id: "nested",
          action: "organization:enterprise_trial_converted",
          actorId: "actor-id",
          actorSlug: "actor-slug",
          subjectId: "subject-id",
          projectId: "project-id",
          actingClientId: "client-id",
          metadata: { conversion_source: "manual", extra: { retained: true } },
          beforeSnapshot: {
            organization: { account_type: "free", whitelisted: false },
            trial: { status: "running", tier: "enterprise" },
            trial_ends_at: "2026-08-20T12:00:00Z",
            keys: [{ key_type: "chat", effective_disabled: false }],
            generic_extra: { value: "before" },
          },
          afterSnapshot: {
            organization: { account_type: "enterprise", whitelisted: true },
            trial: { status: "converted", tier: "enterprise" },
            trial_ends_at: "2026-08-27T12:00:00Z",
            keys: [{ key_type: "chat", effective_disabled: true }],
            generic_extra: { value: "after" },
          },
        }),
      ],
    });

    await renderWithApp(<Activity org={ORG} />);
    fireEvent.click(await screen.findByRole("button", { name: "Show diff ▾" }));
    const diff = screen.getByRole("region", { name: "Changed fields" });
    for (const label of [
      "Account type",
      "Whitelisted",
      "Trial status",
      "Trial end",
      "OpenRouter effective disabled (chat)",
      "generic_extra",
    ]) {
      expect(within(diff).getByText(label)).toBeTruthy();
    }

    fireEvent.click(screen.getByText(/Event details for/));
    for (const value of [
      "actor-id",
      "actor-slug",
      "subject-id",
      "project-id",
      "client-id",
    ]) {
      expect(screen.getAllByText(value).length).toBeGreaterThan(0);
    }
    expect(screen.getByText(/"retained": true/)).toBeTruthy();
    fireEvent.click(
      within(diff).getByRole("button", { name: "View raw diff" }),
    );
    expect(screen.getAllByText(/"generic_extra"/)).toHaveLength(2);
  });

  it("deduplicates IDs at page boundaries and announces incremental loading", async () => {
    const secondPage = deferred<{ logs: ReturnType<typeof anActivityLog>[] }>();
    mocks.listOrganizationActivity
      .mockResolvedValueOnce({
        logs: [anActivityLog({ id: "same", action: "first-seen" })],
        nextCursor: "next",
      })
      .mockReturnValueOnce(secondPage.promise);
    await renderWithApp(<Activity org={ORG} />);

    fireEvent.click(await screen.findByRole("button", { name: "Load more" }));
    expect((await screen.findByRole("status")).textContent).toBe(
      "Loading more activity",
    );
    secondPage.resolve({
      logs: [
        anActivityLog({ id: "same", action: "duplicate" }),
        anActivityLog({ id: "new", action: "new" }),
      ],
    });
    await screen.findByText("new");
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    expect(screen.getByText("first-seen")).toBeTruthy();
    expect(screen.queryByText("duplicate")).toBeNull();
  });

  it("paginates a new organization while the previous page is still pending", async () => {
    const page = deferred<{ logs: ReturnType<typeof anActivityLog>[] }>();
    const otherOrg = anOrganization({ id: "other-org" });
    mocks.listOrganizationActivity
      .mockResolvedValueOnce({
        logs: [anActivityLog({ action: "old-event" })],
        nextCursor: "old-next",
      })
      .mockReturnValueOnce(page.promise)
      .mockResolvedValueOnce({
        logs: [anActivityLog({ action: "new-event" })],
        nextCursor: "new-next",
      })
      .mockResolvedValueOnce({
        logs: [anActivityLog({ id: "next-event", action: "next-event" })],
      });
    function SwitchOrganization() {
      const [org, setOrg] = useState(ORG);
      return (
        <>
          <button onClick={() => setOrg(otherOrg)}>Switch organization</button>
          <Activity org={org} />
        </>
      );
    }
    await renderWithApp(<SwitchOrganization />);
    fireEvent.click(await screen.findByRole("button", { name: "Load more" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Switch organization" }),
    );
    await screen.findByText("new-event");
    fireEvent.click(screen.getByRole("button", { name: "Load more" }));
    try {
      await screen.findByText("next-event");
      expect(mocks.listOrganizationActivity).toHaveBeenLastCalledWith(
        otherOrg.id,
        "new-next",
      );
      expect(screen.queryByText("old-event")).toBeNull();
    } finally {
      page.resolve({ logs: [] });
    }
  });

  it("guards load-more synchronously, handles rejection, and safely settles after unmount", async () => {
    const page = deferred<{ logs: ReturnType<typeof anActivityLog>[] }>();
    mocks.listOrganizationActivity
      .mockResolvedValueOnce({
        logs: [anActivityLog({ id: "current" })],
        nextCursor: "next",
      })
      .mockReturnValueOnce(page.promise);
    const mounted = await renderWithApp(<Activity org={ORG} />);
    const loadMore = await screen.findByRole("button", { name: "Load more" });

    fireEvent.click(loadMore);
    fireEvent.click(loadMore);
    expect(mocks.listOrganizationActivity).toHaveBeenCalledTimes(2);

    mounted.unmount();
    page.reject(new Error("offline"));
    await page.promise.catch(() => undefined);
  });

  it("exposes the initial loading message as a polite status", async () => {
    const pending = deferred<{ logs: ReturnType<typeof anActivityLog>[] }>();
    mocks.listOrganizationActivity.mockReturnValue(pending.promise);
    const mounted = await renderWithApp(<Activity org={ORG} />);
    const status = await screen.findByRole("status");
    expect(status.getAttribute("aria-live")).toBe("polite");
    expect(status.textContent).toContain("Loading activity");
    mounted.unmount();
    pending.resolve({ logs: [] });
  });

  it("keeps rows and prevents repeated activation while retrying a later page", async () => {
    const retry = deferred<{ logs: ReturnType<typeof anActivityLog>[] }>();
    mocks.listOrganizationActivity
      .mockResolvedValueOnce({
        logs: [anActivityLog({ id: "event-2", action: "event-2" })],
        nextCursor: "retry-cursor",
      })
      .mockRejectedValueOnce(new Error("offline"))
      .mockReturnValueOnce(retry.promise);
    await renderWithApp(<Activity org={ORG} />);
    fireEvent.click(await screen.findByRole("button", { name: "Load more" }));

    expect((await screen.findByRole("alert")).textContent).toContain(
      "Unable to load more activity",
    );
    expect(screen.getByText("event-2")).toBeTruthy();
    expect(screen.queryByText(/^Unable to load activity$/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry loading more" }));

    const retrying = await screen.findByRole("button", { name: "Retrying..." });
    expect(retrying.hasAttribute("disabled")).toBe(true);
    expect(screen.getByRole("alert").textContent).toContain(
      "Unable to load more activity",
    );
    fireEvent.click(retrying);
    expect(mocks.listOrganizationActivity).toHaveBeenCalledTimes(3);

    retry.resolve({
      logs: [anActivityLog({ id: "event-1", action: "event-1" })],
    });
    await screen.findByText("event-1");
    expect(screen.getAllByText("event-1")).toHaveLength(1);
    expect(screen.getByText("event-2")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(mocks.listOrganizationActivity).toHaveBeenLastCalledWith(
      ORG.id,
      "retry-cursor",
    );
  });
});
