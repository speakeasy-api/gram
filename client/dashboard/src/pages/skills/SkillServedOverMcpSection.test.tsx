import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  grants: [] as Array<{
    scope: string;
    selectors: Array<Record<string, string>>;
  }>,
  links: [] as unknown[],
  linkMutate: vi.fn(),
  unlinkMutate: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: "session-a" }),
  useProject: () => ({ id: "project-a", slug: "project-a" }),
  useOrganization: () => ({ id: "org-a" }),
}));
vi.mock("@/hooks/useRBAC", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useRBAC")>()),
  useRBAC: () => ({ grants: state.grants, isLoading: false }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      details: { skills: { href: (slug: string) => `/mcp/${slug}/skills` } },
      gateway: {
        skills: { href: (id: string) => `/mcp/gateway/${id}/skills` },
      },
    },
  }),
  useOrgRoutes: () => ({ gateway: { href: () => "/org/gateway" } }),
}));
vi.mock("@/hooks/useDrainInfiniteQuery", () => ({
  useDrainInfiniteQuery: () => undefined,
}));
vi.mock("@/pages/plugins/usePluginServerQueries", () => ({
  usePluginServerQueries: () => ({
    toolsetsQuery: {
      data: { toolsets: [{ id: "toolset-a", slug: "billing" }] },
    },
    serversQuery: {
      data: {
        mcpServers: [
          { id: "toolset-a", toolsetId: "toolset-a" },
          { id: "wrapper-2", toolsetId: "toolset-a" },
        ],
      },
    },
  }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock("@gram/client/react-query/skillLinks.js", () => ({
  useSkillLinksInfinite: () => ({
    data: { pages: [{ result: { links: state.links } }] },
    hasNextPage: false,
    error: null,
  }),
  invalidateAllSkillLinks: vi.fn(),
}));
vi.mock("@gram/client/react-query/linkSkill.js", () => ({
  useLinkSkillMutation: () => ({ mutate: state.linkMutate, isPending: false }),
}));
vi.mock("@gram/client/react-query/unlinkSkill.js", () => ({
  useUnlinkSkillMutation: () => ({
    mutate: state.unlinkMutate,
    isPending: false,
  }),
}));

import { SkillServedOverMcpSection } from "./SkillServedOverMcpSection";

function link(id: string, overrides: Record<string, unknown>) {
  return {
    id,
    skillId: "skill-a",
    skillName: "skill-a",
    skillDisplayName: "Skill A",
    createdAt: new Date(),
    ...overrides,
  };
}

function renderSection() {
  return render(
    <TooltipProvider>
      <MemoryRouter>
        <SkillServedOverMcpSection
          skillId="skill-a"
          skillDisplayName="Skill A"
        />
      </MemoryRouter>
    </TooltipProvider>,
  );
}

const projectWrite = {
  scope: "project:write",
  selectors: [{ resourceId: "project-a" }],
};

afterEach(cleanup);
beforeEach(() => {
  state.grants = [];
  state.linkMutate.mockReset();
  state.unlinkMutate.mockReset();
  state.links = [
    link("l1", {
      targetKind: "mcp_server",
      mcpServerId: "toolset-a",
      targetName: "Billing",
    }),
    link("l2", {
      targetKind: "mcp_server",
      mcpServerId: "wrapper-2",
      targetName: "Billing (team)",
    }),
    link("l3", {
      targetKind: "meta_mcp_server",
      metaMcpServerId: "gw",
      targetName: "Ops gateway",
    }),
  ];
});

describe("SkillServedOverMcpSection", () => {
  it("links each server and gateway to the tab that manages it", () => {
    renderSection();
    expect(
      screen.getByRole("link", { name: "Billing" }).getAttribute("href"),
    ).toBe("/mcp/billing/skills");
    expect(
      screen.getByRole("link", { name: "Ops gateway" }).getAttribute("href"),
    ).toBe("/mcp/gateway/gw/skills");
    // Another server built on the toolset has no Skills tab of its own.
    expect(screen.queryByRole("link", { name: "Billing (team)" })).toBeNull();
    expect(screen.getByText("Billing (team)")).toBeDefined();
  });

  it("adds the skill to the organization gateway for project writers", () => {
    state.grants = [projectWrite];
    renderSection();
    fireEvent.click(screen.getByText("Add to organization gateway"));
    expect(state.linkMutate).toHaveBeenCalledWith(
      {
        request: {
          linkSkillRequestBody: {
            id: "skill-a",
            targetKind: "organization_gateway",
          },
        },
      },
      expect.anything(),
    );
  });

  it("hides the organization gateway action once the skill is served there", () => {
    state.grants = [projectWrite];
    state.links = [link("l4", { targetKind: "organization_gateway" })];
    renderSection();
    expect(screen.queryByText("Add to organization gateway")).toBeNull();
    fireEvent.click(screen.getByLabelText("Remove from Organization gateway"));
    expect(state.unlinkMutate).toHaveBeenCalledWith(
      { request: { unlinkSkillRequestBody: { linkId: "l4" } } },
      expect.anything(),
    );
  });

  it("hides write controls from readers", () => {
    renderSection();
    expect(screen.queryByText("Add to organization gateway")).toBeNull();
    expect(screen.queryByLabelText("Remove from Billing")).toBeNull();
  });
});
