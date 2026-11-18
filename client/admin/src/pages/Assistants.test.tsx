import { createRootRoute, createRoute } from "@tanstack/react-router";
import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AdminOrganizationAssistant } from "@/lib/gramAdminApi";
import { renderRouteTree } from "@/test/harness";
import { AssistantsPage } from "./Assistants";

const api = vi.hoisted(() => ({
  organizations: vi.fn(),
  assistants: vi.fn(),
  repair: vi.fn(),
  identity: vi.fn(),
  kit: vi.fn(),
}));
vi.mock("@/lib/gramAdminApi", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/gramAdminApi")>()),
  listAssistantMigrationOrganizations: api.organizations,
  listOrganizationAssistants: api.assistants,
  repairAssistantAgentNames: api.repair,
  migrateOrganizationAssistantIdentities: api.identity,
  migrateOrganizationAssistantsToKit: api.kit,
  getSession: vi.fn().mockResolvedValue({ user_id: "staff-fixture" }),
}));

function assistant(
  id: string,
  overrides: Partial<AdminOrganizationAssistant> = {},
): AdminOrganizationAssistant {
  return {
    id,
    name: `Assistant ${id}`,
    project_id: "project-fixture",
    project_slug: "fixture",
    project_name: "Fixture",
    status: "active",
    identity_state: "ACTIVE",
    runtime_version: 2,
    creator_active: true,
    agent_name_repair_state: "pending",
    created_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}
const organizations = [
  {
    id: "org-alpha",
    name: "Fixture Alpha",
    slug: "fixture-alpha",
    assistants: 1,
  },
  { id: "org-beta", name: "Fixture Beta", slug: "fixture-beta", assistants: 1 },
];
async function mount(initialPath = "/assistants") {
  const root = createRootRoute();
  const route = createRoute({
    getParentRoute: () => root,
    path: "/assistants",
    component: AssistantsPage,
    validateSearch: (search: Record<string, unknown>) => ({
      organization:
        typeof search.organization === "string"
          ? search.organization
          : undefined,
    }),
  });
  return renderRouteTree(root.addChildren([route]), { initialPath });
}
function bulk(label = "Repair names") {
  return screen.getByRole<HTMLButtonElement>("button", {
    name: `${label} · all eligible`,
  });
}
async function ready() {
  await waitFor(() => expect(bulk().disabled).toBe(false));
}
async function confirm(label: string) {
  const dialog = await screen.findByRole("dialog");
  fireEvent.click(within(dialog).getByRole("button", { name: label }));
}
function row(name: string) {
  const element = screen.getByText(name, { exact: true }).closest("tr");
  if (!element) throw new Error(`Missing row for ${name}`);
  return within(element);
}

beforeEach(() => {
  vi.resetAllMocks();
  api.organizations.mockResolvedValue({ organizations });
  api.assistants.mockImplementation((id: string) =>
    Promise.resolve({
      organization_id: id,
      assistants: [assistant(`${id}-assistant`)],
    }),
  );
  api.repair.mockImplementation((id: string) =>
    Promise.resolve({
      organization_id: id,
      repaired: 1,
      conflicts: 0,
      failed: 0,
      results: [],
    }),
  );
  api.identity.mockImplementation((id: string) =>
    Promise.resolve({
      organization_id: id,
      migrated: 1,
      already_migrated: 0,
      needs_owner: 0,
      failed: 0,
      results: [],
    }),
  );
  api.kit.mockImplementation((id: string) =>
    Promise.resolve({
      organization_id: id,
      switched: 1,
      already_kit: 0,
      needs_identity: 0,
      ineligible: 0,
      busy: 0,
      failed: 0,
      results: [],
    }),
  );
});
afterEach(cleanup);

describe("central assistant migrations", () => {
  it("keeps bulk scope across all organizations even with a search filter and confirms before writing", async () => {
    await mount("/assistants?organization=fixture-alpha");
    await ready();
    expect(screen.queryByText("Fixture Beta", { exact: true })).toBeNull();
    fireEvent.click(bulk());
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText("Repair names for 2 organizations?"),
    ).toBeTruthy();
    expect(
      within(dialog).getByText(/Fixture Beta \(fixture-beta\): 1/),
    ).toBeTruthy();
    expect(
      within(dialog).getByText(/regardless of the search filter/),
    ).toBeTruthy();
    expect(api.repair).not.toHaveBeenCalled();
    await confirm("Repair names");
    await waitFor(() => expect(api.repair).toHaveBeenCalledTimes(2));
    expect(api.repair.mock.calls.map(([id]) => id)).toEqual([
      "org-alpha",
      "org-beta",
    ]);
    await ready();
  });

  it("cancels without sending any migration request", async () => {
    await mount();
    await ready();
    fireEvent.click(bulk());
    fireEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "Cancel",
      }),
    );
    await ready();
    expect(api.repair).not.toHaveBeenCalled();
    expect(api.identity).not.toHaveBeenCalled();
    expect(api.kit).not.toHaveBeenCalled();
  });

  it("targets only the chosen organization for identity migration", async () => {
    api.assistants.mockImplementation((id: string) =>
      Promise.resolve({
        organization_id: id,
        assistants: [assistant(id, { identity_state: "NEVER_CONFIGURED" })],
      }),
    );
    await mount();
    await ready();
    fireEvent.click(
      row("Fixture Beta").getByRole("button", { name: "Migrate identities" }),
    );
    expect(
      await screen.findByText("Migrate identities for Fixture Beta?"),
    ).toBeTruthy();
    await confirm("Migrate identities");
    await waitFor(() =>
      expect(api.identity).toHaveBeenCalledExactlyOnceWith("org-beta"),
    );
    await ready();
    expect(api.repair).not.toHaveBeenCalled();
    expect(api.kit).not.toHaveBeenCalled();
  });

  it("sends explicit Kit assistant IDs and excludes already migrated and blocked identities", async () => {
    api.assistants.mockImplementation((id: string) =>
      Promise.resolve({
        organization_id: id,
        assistants: [
          assistant("eligible"),
          assistant("on-kit", { runtime_version: 3 }),
          assistant("no-identity", { identity_state: "NEVER_CONFIGURED" }),
          assistant("unavailable", { identity_state: "UNAVAILABLE" }),
        ],
      }),
    );
    await mount();
    await ready();
    fireEvent.click(
      row("Fixture Alpha").getByRole("button", { name: "Move to Kit" }),
    );
    await confirm("Move to Kit");
    await waitFor(() =>
      expect(api.kit).toHaveBeenCalledExactlyOnceWith("org-alpha", [
        "eligible",
      ]),
    );
    await ready();
  });

  it("blocks bulk when any organization status fails but permits a loaded per-org action", async () => {
    api.assistants.mockImplementation((id: string) =>
      id === "org-beta"
        ? Promise.reject(new Error("Status unavailable"))
        : Promise.resolve({ organization_id: id, assistants: [assistant(id)] }),
    );
    await mount();
    expect(await screen.findByText(/Status unavailable/)).toBeTruthy();
    await waitFor(() =>
      expect(
        row("Fixture Alpha").getByRole<HTMLButtonElement>("button", {
          name: "Repair names",
        }).disabled,
      ).toBe(false),
    );
    for (const label of ["Repair names", "Migrate identities", "Move to Kit"])
      expect(bulk(label).disabled).toBe(true);
    fireEvent.click(
      row("Fixture Alpha").getByRole("button", { name: "Repair names" }),
    );
    await confirm("Repair names");
    await waitFor(() =>
      expect(api.repair).toHaveBeenCalledExactlyOnceWith("org-alpha"),
    );
  });

  it("continues the queue after an organization fails and displays its failure", async () => {
    api.repair.mockRejectedValueOnce(new Error("Fixture repair failure"));
    await mount();
    await ready();
    fireEvent.click(bulk());
    await confirm("Repair names");
    await waitFor(() => expect(api.repair).toHaveBeenCalledTimes(2));
    expect(api.repair.mock.calls.map(([id]) => id)).toEqual([
      "org-alpha",
      "org-beta",
    ]);
    expect(
      await screen.findByText("Last run: Fixture repair failure"),
    ).toBeTruthy();
    await ready();
  });

  it("stops after the in-flight organization without scheduling the next one", async () => {
    let release: (() => void) | undefined;
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    api.repair.mockImplementationOnce(async (id: string) => {
      await pending;
      return {
        organization_id: id,
        repaired: 1,
        conflicts: 0,
        failed: 0,
        results: [],
      };
    });
    await mount();
    await ready();
    fireEvent.click(bulk());
    await confirm("Repair names");
    await waitFor(() =>
      expect(api.repair).toHaveBeenCalledExactlyOnceWith("org-alpha"),
    );
    fireEvent.click(screen.getByRole("button", { name: "Stop after current" }));
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Stopping…" })
        .disabled,
    ).toBe(true);
    expect(api.repair).toHaveBeenCalledTimes(1);
    await act(async () => {
      release?.();
      await pending;
    });
    await ready();
    expect(
      row("Fixture Alpha").getByText(
        "Last run: Repaired 1, conflicts 0, failed 0",
      ),
    ).toBeTruthy();
    expect(api.repair).toHaveBeenCalledExactlyOnceWith("org-alpha");
    expect(row("Fixture Beta").queryByText(/Last run:/)).toBeNull();
  });

  it("does not schedule later organizations after unmounting during the first migration", async () => {
    let release: (() => void) | undefined;
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    api.repair.mockImplementationOnce(async (id: string) => {
      await pending;
      return {
        organization_id: id,
        repaired: 1,
        conflicts: 0,
        failed: 0,
        results: [],
      };
    });
    const { unmount } = await mount();
    await ready();
    fireEvent.click(bulk());
    await confirm("Repair names");
    await waitFor(() =>
      expect(api.repair).toHaveBeenCalledExactlyOnceWith("org-alpha"),
    );
    api.assistants.mockClear();
    unmount();
    await act(async () => {
      release?.();
      await pending;
    });
    await waitFor(() =>
      expect(api.assistants).toHaveBeenCalledWith("org-alpha"),
    );
    expect(api.repair).toHaveBeenCalledExactlyOnceWith("org-alpha");
    expect(api.assistants).not.toHaveBeenCalledWith("org-beta");
  });

  it("refreshes discovery and each organization status", async () => {
    await mount();
    await ready();
    api.assistants.mockClear();
    fireEvent.click(screen.getByRole("button", { name: "Refresh status" }));
    await waitFor(() => expect(api.organizations).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(api.assistants).toHaveBeenCalledTimes(2));
    expect(api.assistants).toHaveBeenCalledWith("org-alpha");
    expect(api.assistants).toHaveBeenCalledWith("org-beta");
    await ready();
  });

  it("bounds status reads to four workers and keeps bulk disabled until discovery completes", async () => {
    const many = Array.from({ length: 7 }, (_, index) => ({
      id: `org-${index}`,
      name: `Fixture ${index}`,
      slug: `fixture-${index}`,
      assistants: 1,
    }));
    api.organizations.mockResolvedValue({ organizations: many });
    const pending: (() => void)[] = [];
    let active = 0;
    let peak = 0;
    api.assistants.mockImplementation(
      (id: string) =>
        new Promise((resolve) => {
          active++;
          peak = Math.max(peak, active);
          pending.push(() => {
            active--;
            resolve({ organization_id: id, assistants: [assistant(id)] });
          });
        }),
    );
    await mount();
    await waitFor(() => expect(api.assistants).toHaveBeenCalledTimes(4));
    expect(bulk().disabled).toBe(true);
    await act(async () => {
      pending.splice(0).forEach((resolve) => resolve());
    });
    await waitFor(() => expect(api.assistants).toHaveBeenCalledTimes(7));
    expect(bulk().disabled).toBe(true);
    await act(async () => {
      pending.splice(0).forEach((resolve) => resolve());
    });
    await ready();
    expect(peak).toBe(4);
  });
});
