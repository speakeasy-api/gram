import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { AdminOrganizationAssistant } from "@/lib/gramAdminApi";
import { routeTree } from "@/routeTree.gen";
import { anOrganization } from "@/test/fixtures";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  getOrganization: vi.fn(),
  listOrganizationProjects: vi.fn(),
  listOrganizationAssistants: vi.fn(),
  migrateOrganizationAssistantIdentities: vi.fn(),
  migrateOrganizationAssistantsToKit: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getSession: mocks.getSession,
    getOrganization: mocks.getOrganization,
    listOrganizationProjects: mocks.listOrganizationProjects,
    listOrganizationAssistants: mocks.listOrganizationAssistants,
    migrateOrganizationAssistantIdentities:
      mocks.migrateOrganizationAssistantIdentities,
    migrateOrganizationAssistantsToKit:
      mocks.migrateOrganizationAssistantsToKit,
  };
});

const ORG = anOrganization();

function anAssistant(
  overrides: Partial<AdminOrganizationAssistant>,
): AdminOrganizationAssistant {
  return {
    id: "assistant-id",
    name: "Assistant",
    project_id: "project-id",
    project_slug: "project",
    project_name: "Project",
    status: "active",
    identity_state: "NEVER_CONFIGURED",
    runtime_version: 2,
    created_by_user_email: "creator@example.test",
    creator_active: true,
    created_at: "2026-01-03T00:00:00Z",
    ...overrides,
  };
}

const LEGACY = anAssistant({ id: "legacy", name: "Legacy assistant" });
const ORPHANED = anAssistant({
  id: "orphaned",
  name: "Orphaned assistant",
  creator_active: false,
});
const CONFIGURED = anAssistant({
  id: "configured",
  name: "Configured assistant",
  identity_state: "ACTIVE",
  runtime_version: 3,
});
const KIT_READY = anAssistant({
  id: "kit-ready",
  name: "Kit ready assistant",
  identity_state: "ACTIVE",
});
const KIT_BUSY = anAssistant({
  id: "kit-busy",
  name: "Busy assistant",
  identity_state: "ACTIVE",
});

beforeEach(() => {
  mocks.getSession.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  mocks.getOrganization.mockReset();
  mocks.getOrganization.mockResolvedValue(ORG);
  mocks.listOrganizationProjects.mockReset();
  mocks.listOrganizationProjects.mockResolvedValue({ projects: [] });
  mocks.listOrganizationAssistants.mockReset();
  mocks.listOrganizationAssistants.mockImplementation(
    (organizationID: string) =>
      organizationID === ORG.id
        ? Promise.resolve({
            organization_id: ORG.id,
            assistants: [LEGACY, ORPHANED, CONFIGURED, KIT_READY, KIT_BUSY],
          })
        : Promise.reject(new Error(`no organization ${organizationID}`)),
  );
  mocks.migrateOrganizationAssistantIdentities.mockReset();
  mocks.migrateOrganizationAssistantIdentities.mockResolvedValue({
    organization_id: ORG.id,
    migrated: 1,
    already_migrated: 1,
    needs_owner: 1,
    failed: 0,
    results: [
      {
        assistant_id: "legacy",
        project_id: "project-id",
        name: LEGACY.name,
        outcome: "migrated",
      },
      {
        assistant_id: "orphaned",
        project_id: "project-id",
        name: ORPHANED.name,
        outcome: "needs_owner",
      },
      {
        assistant_id: "configured",
        project_id: "project-id",
        name: CONFIGURED.name,
        outcome: "already_migrated",
      },
    ],
  });
  mocks.migrateOrganizationAssistantsToKit.mockReset();
  mocks.migrateOrganizationAssistantsToKit.mockResolvedValue({
    organization_id: ORG.id,
    switched: 1,
    already_kit: 0,
    needs_identity: 0,
    ineligible: 0,
    busy: 1,
    failed: 0,
    results: [
      {
        assistant_id: "kit-ready",
        project_id: "project-id",
        name: KIT_READY.name,
        outcome: "switched",
      },
      {
        assistant_id: "kit-busy",
        project_id: "project-id",
        name: KIT_BUSY.name,
        outcome: "busy",
      },
    ],
  });
});

afterEach(cleanup);

describe("Assistants", () => {
  async function renderAssistants(waitFor = LEGACY.name): Promise<void> {
    await renderRouteTree(routeTree, {
      initialPath: `/organizations/${ORG.slug}/assistants`,
    });
    await screen.findByText(waitFor);
  }

  it("lists identity, runtime and creator membership", async () => {
    await renderAssistants();
    const configured = screen.getByText(CONFIGURED.name).closest("tr");
    expect(configured?.textContent).toContain("Agent");
    expect(configured?.textContent).toContain("Kit");
    const orphaned = screen.getByText(ORPHANED.name).closest("tr");
    expect(orphaned?.textContent).toContain("None");
    expect(orphaned?.textContent).toContain("(not a member)");
    expect(
      screen.getByText("5 assistants, 2 without an agent identity"),
    ).toBeTruthy();
  });

  it("migrates after confirmation and reports skipped assistants", async () => {
    await renderAssistants();
    fireEvent.click(
      screen.getByRole("button", { name: "Migrate to agent identity" }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(dialog.textContent).toContain(
      "1 of 2 assistants without an agent identity can be migrated",
    );
    expect(mocks.migrateOrganizationAssistantIdentities).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole("button", { name: "Migrate" }));

    expect(
      await screen.findByText(
        "Migrated 1, already migrated 1, needs owner 1, failed 0",
      ),
    ).toBeTruthy();
    expect(mocks.migrateOrganizationAssistantIdentities).toHaveBeenCalledWith(
      ORG.id,
    );
    expect(
      screen.getByText(
        `${ORPHANED.name}: its creator is no longer a member, so nobody can own its agent`,
      ),
    ).toBeTruthy();
  });

  it("disables the migration when no assistant can be migrated", async () => {
    mocks.listOrganizationAssistants.mockResolvedValue({
      organization_id: ORG.id,
      assistants: [ORPHANED, CONFIGURED],
    });
    await renderAssistants(ORPHANED.name);
    expect(
      (
        screen.getByRole("button", {
          name: "Migrate to agent identity",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("offers Kit only to assistants with an agent identity on the default runtime", async () => {
    await renderAssistants();
    expect(
      screen.getByRole("button", { name: `Migrate ${KIT_READY.name} to Kit` }),
    ).toBeTruthy();
    for (const assistant of [LEGACY, ORPHANED, CONFIGURED]) {
      expect(
        screen.queryByRole("button", {
          name: `Migrate ${assistant.name} to Kit`,
        }),
      ).toBeNull();
    }
  });

  it("migrates one assistant to Kit after confirmation", async () => {
    await renderAssistants();
    fireEvent.click(
      screen.getByRole("button", { name: `Migrate ${KIT_READY.name} to Kit` }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(dialog.textContent).toContain(
      "1 assistant with an agent identity is on the default runtime",
    );
    expect(mocks.migrateOrganizationAssistantsToKit).not.toHaveBeenCalled();

    fireEvent.click(
      within(dialog).getByRole("button", { name: "Migrate to Kit" }),
    );

    expect(
      await screen.findByText(
        "Moved to Kit 1, already on Kit 0, needs identity 0, ineligible 0, busy 1, failed 0",
      ),
    ).toBeTruthy();
    expect(mocks.migrateOrganizationAssistantsToKit).toHaveBeenCalledWith(
      ORG.id,
      [KIT_READY.id],
    );
  });

  it("migrates every eligible assistant to Kit and reports skipped ones", async () => {
    await renderAssistants();
    fireEvent.click(
      screen.getByRole("button", { name: "Migrate eligible to Kit" }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(dialog.textContent).toContain(
      "2 assistants with an agent identity are on the default runtime",
    );

    fireEvent.click(
      within(dialog).getByRole("button", { name: "Migrate to Kit" }),
    );

    expect(
      await screen.findByText(
        `${KIT_BUSY.name}: it may still be working; try again once it has been idle a while`,
      ),
    ).toBeTruthy();
    expect(mocks.migrateOrganizationAssistantsToKit).toHaveBeenCalledWith(
      ORG.id,
      [KIT_READY.id, KIT_BUSY.id],
    );
  });

  it("disables the Kit migration when no assistant is eligible", async () => {
    mocks.listOrganizationAssistants.mockResolvedValue({
      organization_id: ORG.id,
      assistants: [LEGACY, CONFIGURED],
    });
    await renderAssistants();
    expect(
      (
        screen.getByRole("button", {
          name: "Migrate eligible to Kit",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });
});
