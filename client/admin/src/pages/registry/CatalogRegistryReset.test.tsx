import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderWithApp } from "@/test/harness";
import { CatalogRegistryReset } from "./CatalogRegistryReset";
const fetcher = vi.hoisted(() => vi.fn());
vi.mock("@/lib/gramAdminApi", async (original) => ({
  ...(await original<typeof import("@/lib/gramAdminApi")>()),
  gramAdminFetch: fetcher,
  gramAdminMutation: fetcher,
}));
const plan = {
  id: "11111111-1111-4111-8111-111111111111",
  operation: "reset",
  destination: "local",
  confirmation: "RESET local exact-plan",
  scopeWarning: "Testing edits are discarded.",
  sourceIdentity: "production",
  sourceRevision: "rev1",
  expiresAt: "2099-01-01T00:00:00Z",
  changes: [
    { name: "editable", action: "update", reason: "discard_testing_edits" },
    { name: "structural", action: "conflict", reason: "immutable_structure" },
  ],
};
beforeEach(() => {
  fetcher.mockImplementation(async (_path, init) => {
    const req = JSON.parse(init.body);
    return req.action === "preview"
      ? plan
      : req.action === "status"
        ? {}
        : { ...plan, planId: plan.id, state: "completed_with_conflicts" };
  });
});
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});
async function preview() {
  fireEvent.click(
    screen.getByRole("button", { name: "Preview destination reset" }),
  );
  await screen.findByText(plan.confirmation);
}
it("requires exact server confirmation, shows safe operations and conflicts, and never claims full success", async () => {
  await renderWithApp(<CatalogRegistryReset />);
  await preview();
  expect(screen.getByText(/editable/)).toBeDefined();
  expect(screen.getByText(/immutable_structure/)).toBeDefined();
  expect(screen.getByText(/incomplete/i)).toBeDefined();
  const apply = screen.getByRole("button", { name: "Reset destination" });
  fireEvent.change(screen.getByLabelText("Reset confirmation"), {
    target: { value: "yes" },
  });
  expect(apply.hasAttribute("disabled")).toBe(true);
  fireEvent.change(screen.getByLabelText("Reset confirmation"), {
    target: { value: plan.confirmation },
  });
  fireEvent.click(apply);
  await screen.findByText(/not a full reset success/i);
  expect(fetcher).toHaveBeenCalledWith(
    "/admin/catalog.reset",
    expect.objectContaining({
      body: JSON.stringify({
        action: "apply",
        planId: plan.id,
        confirmation: plan.confirmation,
      }),
    }),
  );
});
it("new previews discard typed confirmation", async () => {
  await renderWithApp(<CatalogRegistryReset />);
  await preview();
  fireEvent.change(screen.getByLabelText("Reset confirmation"), {
    target: { value: plan.confirmation },
  });
  await preview();
  expect(
    screen.getByLabelText<HTMLInputElement>("Reset confirmation").value,
  ).toBe("");
  expect(
    screen
      .getByRole("button", { name: "Reset destination" })
      .hasAttribute("disabled"),
  ).toBe(true);
});
it("does not apply expired plans", async () => {
  fetcher.mockResolvedValue({ ...plan, expiresAt: "2000-01-01T00:00:00Z" });
  await renderWithApp(<CatalogRegistryReset />);
  await preview();
  fireEvent.change(screen.getByLabelText("Reset confirmation"), {
    target: { value: plan.confirmation },
  });
  expect(
    screen
      .getByRole("button", { name: "Reset destination" })
      .hasAttribute("disabled"),
  ).toBe(true);
});
it("clears a stale plan without retrying apply", async () => {
  await renderWithApp(<CatalogRegistryReset />);
  await preview();
  fetcher.mockRejectedValue(new Error("stale plan"));
  fireEvent.change(screen.getByLabelText("Reset confirmation"), {
    target: { value: plan.confirmation },
  });
  fireEvent.click(screen.getByRole("button", { name: "Reset destination" }));
  await screen.findAllByText(/No apply retry was attempted/);
  await waitFor(() =>
    expect(screen.queryByLabelText("Reset confirmation")).toBeNull(),
  );
  expect(
    fetcher.mock.calls.filter(
      ([, init]) => JSON.parse(init.body).action === "apply",
    ),
  ).toHaveLength(1);
});

it("labels shared success markers by operation instead of claiming an older refresh was a reset", async () => {
  fetcher.mockResolvedValue({
    lastFullSuccessOperation: "refresh",
    lastFullSuccessAt: "2026-01-01T00:00:00Z",
    lastAttemptOperation: "reset",
    lastAttemptState: "completed_with_conflicts",
  });
  await renderWithApp(<CatalogRegistryReset />);
  await screen.findByText(/Last full catalog success \(refresh\)/);
  expect(
    screen.getByText(
      /Last catalog attempt \(reset\): completed_with_conflicts/,
    ),
  ).toBeDefined();
  expect(screen.queryByText("Destination reset completed.")).toBeNull();
});
