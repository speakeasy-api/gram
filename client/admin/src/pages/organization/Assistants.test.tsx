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
            assistants: [LEGACY, ORPHANED, CONFIGURED],
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
      screen.getByText("3 assistants, 2 without an agent identity"),
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
});
