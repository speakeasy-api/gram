import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { renderWithApp } from "@/test/harness";
import { toast } from "sonner";
import { UserActions } from "./UserActions";
const mocks = vi.hoisted(() => ({
  openOrganizationDashboard: vi.fn(),
  listUserOrganizations: vi.fn(),
}));
vi.mock("@/lib/gramAdminApi", async (original) => ({
  ...(await original<typeof import("@/lib/gramAdminApi")>()),
  ...mocks,
}));
const user = {
  id: "u1",
  display_name: "",
  email: "person@example.test",
  organizations: [],
  organization_count: 0,
};
const org = { id: "canonical-id", name: "Studio", slug: "studio" };
const open = () =>
  fireEvent.pointerDown(
    screen.getByRole("button", { name: "Actions for person@example.test" }),
    { button: 0, ctrlKey: false },
  );
beforeEach(() => vi.clearAllMocks());
describe("User actions", () => {
  it("has copies only for zero organizations and never eagerly fetches", async () => {
    await renderWithApp(<UserActions user={user} />);
    open();
    expect(
      (await screen.findByRole("menuitem", { name: "Copy Name" })).hasAttribute(
        "data-disabled",
      ),
    ).toBe(true);
    expect(
      screen.queryByRole("menuitem", { name: "View Organization" }),
    ).toBeNull();
    expect(mocks.listUserOrganizations).not.toHaveBeenCalled();
  });
  it("targets canonical IDs for direct actions", async () => {
    await renderWithApp(
      <UserActions
        user={{ ...user, organizations: [org], organization_count: 1 }}
      />,
    );
    open();
    expect(
      (
        await screen.findByRole("menuitem", { name: "View Organization" })
      ).getAttribute("href"),
    ).toBe("/organizations/canonical-id");
    fireEvent.click(
      screen.getByRole("menuitem", { name: "Open in Dashboard" }),
    );
    expect(mocks.openOrganizationDashboard).toHaveBeenCalledWith(
      "canonical-id",
    );
  });
  it("disables dashboard, not view, for a disabled organization", async () => {
    await renderWithApp(
      <UserActions
        user={{
          ...user,
          organizations: [{ ...org, disabled_at: "2026-01-01" }],
          organization_count: 1,
        }}
      />,
    );
    open();
    expect(
      (
        await screen.findByRole("menuitem", { name: "Open in Dashboard" })
      ).hasAttribute("data-disabled"),
    ).toBe(true);
    expect(
      screen
        .getByRole("menuitem", { name: "View Organization" })
        .hasAttribute("data-disabled"),
    ).toBe(false);
  });
});

afterEach(cleanup);

it("copies email", async () => {
  const writeText = vi
    .spyOn(navigator.clipboard, "writeText")
    .mockResolvedValue(undefined);
  await renderWithApp(<UserActions user={user} />);
  open();
  fireEvent.click(await screen.findByRole("menuitem", { name: "Copy Email" }));
  expect(writeText).toHaveBeenCalledWith(user.email);
  writeText.mockRestore();
});
it("loads memberships only on opening overflow, dedupes targets and preserves the load control", async () => {
  const preview = [
    org,
    { ...org, id: "second", slug: "second" },
    { ...org, id: "third", slug: "third" },
  ];
  const first = Array.from({ length: 50 }, (_, i) => ({
    ...org,
    id: `org-${i}`,
    slug: `studio-${i}`,
  }));
  mocks.listUserOrganizations
    .mockResolvedValueOnce({
      organizations: first,
      total: 52,
      page: 1,
      limit: 50,
    })
    .mockResolvedValueOnce({
      organizations: [
        first[49],
        { ...org, id: "org-50", slug: "studio-50" },
        { ...org, id: "org-51", slug: "studio-51" },
      ],
      total: 52,
      page: 2,
      limit: 50,
    });
  await renderWithApp(
    <UserActions
      user={{ ...user, organizations: preview, organization_count: 4 }}
    />,
  );
  expect(mocks.listUserOrganizations).not.toHaveBeenCalled();
  open();
  const more = await screen.findByRole("menuitem", {
    name: "Load more organizations",
  });
  more.focus();
  fireEvent.click(more);
  const done = await screen.findByRole("menuitem", {
    name: "All organizations loaded",
  });
  expect(done).toBe(more);
  expect(document.activeElement).toBe(more);
  expect(screen.getAllByRole("menuitem", { name: /Studio \(/ })).toHaveLength(
    52,
  );
  expect(mocks.listUserOrganizations.mock.calls[1]?.[0]).toEqual({
    user_id: "u1",
    page: 2,
    limit: 50,
  });
});
it("retries failed overflow without closing the menu", async () => {
  mocks.listUserOrganizations
    .mockRejectedValueOnce(new Error("Unavailable"))
    .mockResolvedValueOnce({
      organizations: [org],
      total: 1,
      page: 1,
      limit: 50,
    });
  await renderWithApp(
    <UserActions
      user={{ ...user, organizations: [org], organization_count: 4 }}
    />,
  );
  open();
  const retry = await screen.findByRole("menuitem", {
    name: "Retry organizations",
  });
  retry.focus();
  fireEvent.click(retry);
  expect(
    await screen.findByRole("menuitem", { name: "All organizations loaded" }),
  ).toBe(retry);
  expect(document.activeElement).toBe(retry);
});
it("uses submenus for multiple organizations without fetching complete previews", async () => {
  await renderWithApp(
    <UserActions
      user={{
        ...user,
        organizations: [org, { ...org, id: "second", slug: "second" }],
        organization_count: 2,
      }}
    />,
  );
  open();
  const target = await screen.findByRole("menuitem", {
    name: "Studio (second)",
  });
  target.focus();
  fireEvent.keyDown(target, { key: "ArrowRight" });
  expect(
    (
      await screen.findByRole("menuitem", { name: "View Organization" })
    ).getAttribute("href"),
  ).toBe("/organizations/second");
  fireEvent.click(screen.getByRole("menuitem", { name: "Open in Dashboard" }));
  expect(mocks.openOrganizationDashboard).toHaveBeenCalledWith("second");
  expect(mocks.listUserOrganizations).not.toHaveBeenCalled();
});

it("reports clipboard failures without claiming success", async () => {
  const write = vi
    .spyOn(navigator.clipboard, "writeText")
    .mockRejectedValue(new Error("Permission denied"));
  const error = vi.spyOn(toast, "error");
  await renderWithApp(
    <UserActions user={{ ...user, display_name: "Example Person" }} />,
  );
  fireEvent.pointerDown(
    screen.getByRole("button", { name: "Actions for Example Person" }),
    { button: 0, ctrlKey: false },
  );
  fireEvent.click(await screen.findByRole("menuitem", { name: "Copy Name" }));
  await waitFor(() =>
    expect(error).toHaveBeenCalledWith(
      "Could not copy name: Permission denied",
    ),
  );
  expect(write).toHaveBeenCalledWith("Example Person");
  write.mockRestore();
  error.mockRestore();
});
