import { QueryClient } from "@tanstack/react-query";
import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { organizationActivityQuery } from "@/lib/adminQueries";
import { Activity } from "@/pages/organization/Activity";
import { anActivityLog, anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

const mocks = vi.hoisted(() => ({ listOrganizationActivity: vi.fn() }));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    listOrganizationActivity: mocks.listOrganizationActivity,
  };
});

const ORG = anOrganization();

function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

beforeEach(() => mocks.listOrganizationActivity.mockReset());
afterEach(cleanup);

describe("Activity", () => {
  it("renders every event field and the display, slug, and ID fallbacks", async () => {
    const createdAt = "2026-01-15T12:30:00Z";
    mocks.listOrganizationActivity.mockResolvedValue({
      logs: [
        anActivityLog({
          id: "event-display",
          created_at: createdAt,
          actor_id: "actor-display-id",
          actor_type: "staff",
          actor_display_name: "Actor Display",
          actor_slug: "actor-display-slug",
          subject_id: "subject-display-id",
          subject_type: "organization",
          subject_display_name: "Subject Display",
          subject_slug: "subject-display-slug",
          project_id: "project-id",
          project_slug: "project-slug",
          acting_surface: "platform_mcp",
          acting_client_id: "client-id",
          before_snapshot: { days: 7 },
          after_snapshot: { days: 14 },
          metadata: { approved: true },
        }),
        anActivityLog({
          id: "event-slug",
          actor_id: "actor-slug-id",
          actor_display_name: undefined,
          actor_slug: "actor-slug",
          subject_id: "subject-slug-id",
          subject_display_name: undefined,
          subject_slug: "subject-slug",
        }),
        anActivityLog({
          id: "event-id",
          actor_id: "actor-id-fallback",
          actor_display_name: undefined,
          actor_slug: undefined,
          subject_id: "subject-id-fallback",
          subject_display_name: undefined,
          subject_slug: undefined,
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
    expect(time?.getAttribute("datetime")).toBe(createdAt);
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
    expect(screen.getByText(/"days": 7/)).toBeTruthy();
    expect(screen.getByText(/"days": 14/)).toBeTruthy();
    expect(screen.getByText(/"approved": true/)).toBeTruthy();
  });

  it("shows sorted changed, added, removed, and complex fields", async () => {
    mocks.listOrganizationActivity.mockResolvedValue({
      logs: [
        anActivityLog({
          before_snapshot: {
            zeta: "before",
            removed: "gone",
            complex: { nested: ["old"] },
            alpha: null,
          },
          after_snapshot: {
            zeta: "after",
            added: true,
            complex: { nested: ["new"] },
            alpha: false,
          },
        }),
      ],
    });

    await renderWithApp(<Activity org={ORG} />);
    fireEvent.click(
      await screen.findByText(
        "Event details for organization:settings_updated",
      ),
    );

    const table = screen.getByRole("table", { name: "Changed fields" });
    expect(
      within(table)
        .getAllByRole("row")
        .slice(1)
        .map((row) => within(row).getAllByRole("cell")[0]?.textContent),
    ).toEqual(["added", "alpha", "complex", "removed", "zeta"]);

    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows[0]?.textContent).toContain("(none)true");
    expect(rows[1]?.textContent).toContain("nullfalse");
    expect(rows[2]?.textContent).toContain(
      '{"nested":["old"]}{"nested":["new"]}',
    );
    expect(rows[3]?.textContent).toContain("gone(none)");
    expect(rows[4]?.textContent).toContain("beforeafter");
  });

  it("shows no-change copy, raw snapshots disclosure, and separate metadata", async () => {
    const snapshot = { unchanged: "same" };
    mocks.listOrganizationActivity.mockResolvedValue({
      logs: [
        anActivityLog({
          before_snapshot: snapshot,
          after_snapshot: snapshot,
          metadata: { request_id: "request-placeholder" },
        }),
      ],
    });

    await renderWithApp(<Activity org={ORG} />);
    fireEvent.click(
      await screen.findByText(
        "Event details for organization:settings_updated",
      ),
    );

    expect(screen.getByText("No changed fields.")).toBeTruthy();
    expect(screen.queryByRole("table", { name: "Changed fields" })).toBeNull();

    const rawSummary = screen.getByText("Raw snapshots");
    const rawDetails = rawSummary.closest("details");
    expect(rawDetails?.hasAttribute("open")).toBe(false);
    expect(rawDetails?.textContent).not.toContain("request-placeholder");
    fireEvent.click(rawSummary);
    expect(rawDetails?.hasAttribute("open")).toBe(true);
    expect(rawDetails?.textContent).toContain('"unchanged": "same"');

    expect(screen.getByText("Metadata")).toBeTruthy();
    expect(
      screen.getByText(/"request_id": "request-placeholder"/),
    ).toBeTruthy();
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
        next_cursor: "cursor-2",
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

  it("keeps rows and prevents repeated activation while retrying a later page", async () => {
    const retry = deferred<{ logs: ReturnType<typeof anActivityLog>[] }>();
    mocks.listOrganizationActivity
      .mockResolvedValueOnce({
        logs: [anActivityLog({ id: "event-2", action: "event-2" })],
        next_cursor: "retry-cursor",
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
