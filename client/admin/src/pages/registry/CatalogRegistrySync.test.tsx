import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderWithApp } from "@/test/harness";
import { CatalogRegistrySync } from "./CatalogRegistrySync";

const fetcher = vi.hoisted(() => vi.fn());
vi.mock("@/lib/gramAdminApi", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/gramAdminApi")>()),
  gramAdminFetch: fetcher,
  gramAdminMutation: fetcher,
}));
const plan = {
  id: "00000000-0000-4000-8000-000000000001",
  sourceIdentity: "production",
  sourceRevision: "revision-1",
  expiresAt: "2099-01-01T00:00:00Z",
  changes: [
    "create",
    "update",
    "unpublish",
    "republish",
    "adopt",
    "preserved",
    "conflict",
  ].map((action) => ({ name: `example.test/${action}`, action })),
};
beforeEach(() => {
  fetcher.mockImplementation(async (_path, init) => {
    const request = JSON.parse(init.body);
    if (request.action === "status")
      return {
        sourceIdentity: "production",
        lastFullSuccessRevision: "previous-revision",
      };
    if (request.action === "preview") return plan;
    return {
      planId: plan.id,
      sourceRevision: plan.sourceRevision,
      state: "completed_with_conflicts",
      completedAt: "2026-01-01T00:00:00Z",
      changes: plan.changes,
    };
  });
});
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});
async function preview() {
  await renderWithApp(<CatalogRegistrySync />);
  fireEvent.click(
    await screen.findByRole("button", { name: "Preview from production" }),
  );
  await screen.findByText(plan.id);
}
it("requires exact-plan confirmation, groups changes and preserves conflicts", async () => {
  await preview();
  for (const change of plan.changes)
    expect(screen.getByText(change.name)).toBeDefined();
  for (const label of [
    "Create",
    "Update",
    "Unpublish (not delete)",
    "Republish",
    "Adopt",
    "Preserved",
    "Conflicts",
  ]) {
    expect(screen.getByRole("heading", { name: `${label} (1)` })).toBeDefined();
  }
  const apply = screen.getByRole("button", { name: "Apply confirmed plan" });
  expect(apply.hasAttribute("disabled")).toBe(true);
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(apply);
  await screen.findByText("Applied with conflicts — not a full sync success.");
  const calls = fetcher.mock.calls.map(([, init]) => JSON.parse(init.body));
  expect(calls.filter((c) => c.action === "apply")).toEqual([
    { action: "apply", planId: plan.id },
  ]);
  expect(
    screen.queryByRole("button", { name: "Apply confirmed plan" }),
  ).toBeNull();
});
it("discards a stale plan and never retries apply automatically", async () => {
  const { GramAdminError } = await import("@/lib/gramAdminApi");
  await preview();
  fetcher.mockImplementation(async (_path, init) => {
    if (JSON.parse(init.body).action === "apply")
      throw new GramAdminError(409, {}, "stale");
    return { sourceIdentity: "production" };
  });
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(screen.getByRole("button", { name: "Apply confirmed plan" }));
  await screen.findByText(/Plan is stale or unavailable/);
  expect(screen.queryByRole("checkbox")).toBeNull();
  await waitFor(() =>
    expect(
      fetcher.mock.calls.filter(
        ([, init]) => JSON.parse(init.body).action === "apply",
      ),
    ).toHaveLength(1),
  );
  expect(
    screen
      .getByRole("button", { name: "Preview from production" })
      .hasAttribute("disabled"),
  ).toBe(false);
});
it("explains disabled sources without credential inputs", async () => {
  const { GramAdminError } = await import("@/lib/gramAdminApi");
  fetcher.mockRejectedValue(new GramAdminError(503, {}, "unavailable"));
  await renderWithApp(<CatalogRegistrySync />);
  await screen.findByText(/Ask an operator to configure/);
  expect(screen.queryByRole("textbox")).toBeNull();
});
it("blocks expired previews and explains unsupported deployments", async () => {
  fetcher.mockImplementation(async (_path, init) =>
    JSON.parse(init.body).action === "preview"
      ? { ...plan, expiresAt: "2000-01-01T00:00:00Z" }
      : { sourceIdentity: "production" },
  );
  await preview();
  await screen.findByText(
    "Plan expired. Preview again and confirm the new plan.",
  );
  expect(
    screen
      .getByRole("button", { name: "Apply confirmed plan" })
      .hasAttribute("disabled"),
  ).toBe(true);
});
it("explains server-enforced deployment guards", async () => {
  const { GramAdminError } = await import("@/lib/gramAdminApi");
  fetcher.mockRejectedValue(new GramAdminError(403, {}, "forbidden"));
  await renderWithApp(<CatalogRegistrySync />);
  await screen.findByText(/production destinations are blocked/);
});
it("shows only synchronous pending stages until the server returns", async () => {
  await preview();
  let finish!: (value: unknown) => void;
  fetcher.mockImplementation((_path, init) =>
    JSON.parse(init.body).action === "apply"
      ? new Promise((resolve) => {
          finish = resolve;
        })
      : Promise.resolve({ sourceIdentity: "production" }),
  );
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(screen.getByRole("button", { name: "Apply confirmed plan" }));
  await screen.findByText(
    "Applying confirmed plan — waiting for the server result…",
  );
  expect(screen.queryByText("Full sync completed.")).toBeNull();
  expect(
    screen
      .getByRole("button", { name: "Preview from production" })
      .hasAttribute("disabled"),
  ).toBe(true);
  finish({
    planId: plan.id,
    sourceRevision: plan.sourceRevision,
    state: "completed",
    completedAt: "2026-01-01T00:00:00Z",
    changes: [],
  });
  await screen.findByText("Full sync completed.");
});
it("does not offer apply when preview encounters an expired staff session", async () => {
  const { GramAdminError } = await import("@/lib/gramAdminApi");
  fetcher.mockImplementation(async (_path, init) => {
    if (JSON.parse(init.body).action === "preview")
      throw new GramAdminError(401, {}, "expired");
    return { sourceIdentity: "production" };
  });
  await renderWithApp(<CatalogRegistrySync />);
  fireEvent.click(
    screen.getByRole("button", { name: "Preview from production" }),
  );
  await screen.findByText(/Your staff session expired/);
  expect(
    screen.queryByRole("button", { name: "Apply confirmed plan" }),
  ).toBeNull();
  expect(
    fetcher.mock.calls.filter(
      ([, init]) => JSON.parse(init.body).action === "apply",
    ),
  ).toHaveLength(0);
});
it("requires fresh confirmation for a newly previewed ID and revision", async () => {
  await preview();
  fireEvent.click(screen.getByRole("checkbox"));
  const next = {
    ...plan,
    id: "00000000-0000-4000-8000-000000000002",
    sourceRevision: "revision-2",
  };
  fetcher.mockResolvedValue(next);
  fireEvent.click(
    screen.getByRole("button", { name: "Preview from production" }),
  );
  await screen.findByText(next.id);
  expect(screen.getByText(next.sourceRevision)).toBeDefined();
  expect(screen.getByRole("checkbox").getAttribute("aria-checked")).toBe(
    "false",
  );
  expect(
    screen
      .getByRole("button", { name: "Apply confirmed plan" })
      .hasAttribute("disabled"),
  ).toBe(true);
  expect(
    fetcher.mock.calls.filter(
      ([, init]) => JSON.parse(init.body).action === "apply",
    ),
  ).toHaveLength(0);
});
it.each(["failed", "completed_with_conflicts"])(
  "retains the last full success beside a later %s attempt",
  async (state) => {
    fetcher.mockResolvedValue({
      sourceIdentity: "production",
      lastFullSuccessAt: "2026-01-01T00:00:00Z",
      lastFullSuccessRevision: "successful-revision",
      lastAttemptState: state,
      lastAttemptRevision: "later-revision",
      conflicts:
        state === "completed_with_conflicts"
          ? [{ name: "example.test/conflict", action: "conflict" }]
          : [],
      failure: state === "failed" ? "source_unavailable" : undefined,
    });
    await renderWithApp(<CatalogRegistrySync />);
    await screen.findByText("Last full success: 2026-01-01T00:00:00Z");
    expect(
      screen.getByText("Last full success revision: successful-revision"),
    ).toBeDefined();
    expect(screen.getByText(`Last attempt: ${state}`)).toBeDefined();
    expect(
      screen.getByText("Last attempt revision: later-revision"),
    ).toBeDefined();
    expect(screen.queryByText("Full sync completed.")).toBeNull();
    if (state === "failed")
      expect(screen.getAllByRole("alert")[0]?.textContent).toContain(
        "source_unavailable",
      );
    else expect(screen.getByText("Unresolved conflicts: 1")).toBeDefined();
  },
);

it("expires an open confirmed plan as the subscribed clock advances", async () => {
  vi.useFakeTimers({ toFake: ["Date", "setInterval", "clearInterval"] });
  vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
  try {
    fetcher.mockImplementation(async (_path, init) =>
      JSON.parse(init.body).action === "preview"
        ? { ...plan, expiresAt: "2026-01-01T00:00:02Z" }
        : { sourceIdentity: "production" },
    );
    await preview();
    fireEvent.click(screen.getByRole("checkbox"));
    const apply = screen.getByRole("button", { name: "Apply confirmed plan" });
    expect(apply.hasAttribute("disabled")).toBe(false);
    await act(async () => {
      vi.advanceTimersByTime(2000);
    });
    expect(
      screen.getByText("Plan expired. Preview again and confirm the new plan."),
    ).toBeDefined();
    expect(apply.hasAttribute("disabled")).toBe(true);
    fireEvent.click(apply);
    expect(
      fetcher.mock.calls.filter(
        ([, init]) => JSON.parse(init.body).action === "apply",
      ),
    ).toHaveLength(0);
  } finally {
    cleanup();
    vi.useRealTimers();
  }
});

it("refresh apply updates the mounted reset panel's shared status", async () => {
  let applied = false;
  fetcher.mockImplementation(async (_path, init) => {
    const request = JSON.parse(init.body);
    if (request.action === "preview") return plan;
    if (request.action === "apply") {
      applied = true;
      return { planId: plan.id, state: "completed", changes: [] };
    }
    return {
      sourceIdentity: "production",
      lastAttemptOperation: applied ? "refresh" : "reset",
      lastAttemptState: applied ? "completed" : "completed_with_conflicts",
    };
  });
  await preview();
  await screen.findByText(
    "Last catalog attempt (reset): completed_with_conflicts",
  );
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(screen.getByRole("button", { name: "Apply confirmed plan" }));
  await screen.findByText("Last catalog attempt (refresh): completed");
});
