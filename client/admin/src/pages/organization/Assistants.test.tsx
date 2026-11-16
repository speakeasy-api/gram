import { cleanup, screen } from "@testing-library/react";
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
  mocks.migrateOrganizationAssistantsToKit.mockReset();
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

  it("links to centralized migrations scoped to this organization", async () => {
    await renderAssistants();
    const link = screen.getByRole("link", {
      name: "Manage assistant migrations",
    });
    const destination = new URL(
      link.getAttribute("href")!,
      window.location.origin,
    );
    expect(destination.pathname).toBe("/assistants");
    expect(destination.searchParams.get("organization")).toBe(ORG.id);
    expect(screen.queryByRole("button", { name: /migrate/i })).toBeNull();
    expect(mocks.migrateOrganizationAssistantIdentities).not.toHaveBeenCalled();
    expect(mocks.migrateOrganizationAssistantsToKit).not.toHaveBeenCalled();
  });

  it("keeps migration management available without assistants", async () => {
    mocks.listOrganizationAssistants.mockResolvedValue({
      organization_id: ORG.id,
      assistants: [],
    });
    await renderAssistants("No assistants in this organization");
    expect(
      screen.getByRole("link", { name: "Manage assistant migrations" }),
    ).toBeTruthy();
  });
});
