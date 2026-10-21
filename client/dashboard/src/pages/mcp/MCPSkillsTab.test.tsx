import { TooltipProvider } from "@/components/ui/Tooltip";
import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  flag: { status: "enabled" } as FeatureFlagResult,
  grants: [] as Array<{
    scope: string;
    selectors: Array<Record<string, string>>;
  }>,
  rbacLoading: false,
  links: vi.fn(),
  skills: vi.fn(),
  fetchNextPage: vi.fn(),
  refetchLinks: vi.fn(),
  unlink: vi.fn(),
  picker: vi.fn(),
}));

vi.mock("@/components/page-layout", () => {
  const Section = ({ children }: { children: ReactNode }) => (
    <section>{children}</section>
  );
  return {
    Page: {
      Section: Object.assign(Section, {
        Title: Section,
        Description: Section,
        Body: Section,
        CTA: Section,
      }),
    },
  };
});
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => state.flag,
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: "session-a" }),
  useProject: () => ({ id: "project-a", slug: "project-a" }),
  useOrganization: () => ({ id: "org-a" }),
}));
vi.mock("@/hooks/useRBAC", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/hooks/useRBAC")>();
  type Scope = Parameters<typeof original.hasScopeInGrants>[1];
  const hasScope = (scope: Scope, resourceId?: string, projectId?: string) =>
    original.hasScopeInGrants(
      state.grants as never,
      scope,
      resourceId,
      projectId,
    );
  return {
    ...original,
    useRBAC: () => ({
      grants: state.grants,
      isLoading: state.rbacLoading,
      hasScope,
      hasAnyScope: (scopes: Scope[], resourceId?: string, projectId?: string) =>
        scopes.some((scope) => hasScope(scope, resourceId, projectId)),
      hasAllScopes: (
        scopes: Scope[],
        resourceId?: string,
        projectId?: string,
      ) => scopes.every((scope) => hasScope(scope, resourceId, projectId)),
    }),
  };
});
vi.mock("@gram/client/react-query/requestAccess.js", () => ({
  useRequestAccessMutation: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    skills: { detail: { href: (id: string) => `/skills/${id}` } },
    mcp: {
      details: {
        overview: { href: (slug: string) => `/mcp/${slug}/overview` },
      },
    },
  }),
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@gram/client/react-query/skillLinks.js", () => ({
  useSkillLinksInfinite: state.links,
  invalidateAllSkillLinks: vi.fn(),
}));
vi.mock("@gram/client/react-query/skills.js", () => ({
  useSkillsInfinite: state.skills,
}));
vi.mock("@gram/client/react-query/unlinkSkill.js", () => ({
  useUnlinkSkillMutation: () => ({ mutate: state.unlink, isPending: false }),
}));
vi.mock("@/pages/skills/SkillPickerDialog", () => ({
  SkillPickerDialog: (props: {
    open: boolean;
    target: { linkTarget?: { kind: string; mcpServerId?: string } };
    excludedSkillIds: string[];
  }) => {
    state.picker(props);
    return props.open ? (
      <div data-testid="picker">{props.target.linkTarget?.mcpServerId}</div>
    ) : null;
  },
}));
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  Navigate: ({ to }: { to: string }) => <div data-testid="navigate">{to}</div>,
}));

import { MCPSkillsTab, MCPSkillsTabRoute } from "./MCPSkillsTab";

const toolset = { id: "toolset-a", slug: "toolset-a", name: "Billing" };

function link(id: string, overrides: Record<string, unknown> = {}) {
  return {
    id: `link-${id}`,
    skillId: `skill-${id}`,
    skillName: `skill-${id}`,
    skillDisplayName: `Skill ${id}`,
    targetKind: "mcp_server",
    mcpServerId: "toolset-a",
    targetName: "Billing",
    createdAt: new Date(),
    ...overrides,
  };
}

function infinite(
  pages: unknown[],
  overrides: Record<string, unknown> = {},
): Record<string, unknown> {
  const result = {
    data: { pages },
    hasNextPage: false,
    isFetchingNextPage: false,
    error: null as unknown,
    fetchNextPage: state.fetchNextPage,
    refetch: state.refetchLinks,
    ...overrides,
  };
  return { ...result, isError: !!result.error };
}

function renderTab(ui: ReactNode = <MCPSkillsTab toolset={toolset} />) {
  return render(
    <TooltipProvider>
      <MemoryRouter>{ui}</MemoryRouter>
    </TooltipProvider>,
  );
}

const readSkills = {
  scope: "skill:read",
  selectors: [{ resourceId: "project-a" }],
};
const writeServer = {
  scope: "mcp:write",
  selectors: [{ resourceId: "toolset-a" }],
};

afterEach(cleanup);
beforeEach(() => {
  state.flag = { status: "enabled" };
  state.rbacLoading = false;
  state.grants = [readSkills];
  state.unlink.mockReset();
  state.picker.mockReset();
  state.fetchNextPage.mockReset();
  state.refetchLinks.mockReset();
  state.links
    .mockReset()
    .mockReturnValue(infinite([{ result: { links: [link("2"), link("1")] } }]));
  state.skills.mockReset().mockReturnValue(
    infinite([
      {
        result: {
          skills: [
            {
              id: "skill-1",
              lastLoadedOverMcpAt: new Date(Date.now() - 5 * 60_000),
            },
            { id: "skill-2" },
          ],
        },
      },
    ]),
  );
});

describe("MCPSkillsTab", () => {
  it("lists the skills linked to this server's own row, sorted by name", () => {
    renderTab();
    const names = screen
      .getAllByRole("link")
      .map((element) => element.textContent);
    expect(names).toEqual(["Skill 1", "Skill 2"]);
    expect(state.links).toHaveBeenCalledWith(
      expect.objectContaining({
        mcpServerId: "toolset-a",
        targetKind: "mcp_server",
      }),
      undefined,
      expect.anything(),
    );
  });

  it("reads each skill's last load over MCP from the skill", () => {
    renderTab();
    const row = (name: string) =>
      screen.getByRole("link", { name }).closest("tr")?.textContent ?? "";
    expect(row("Skill 1")).toMatch(/5 minutes ago/);
    expect(row("Skill 1")).not.toMatch(/Never/);
    expect(row("Skill 2")).toMatch(/Never/);
  });

  it("hides add and remove from readers who can't change the server", () => {
    renderTab();
    expect(screen.queryByText("Add skill")).toBeNull();
    expect(screen.queryByLabelText("Remove Skill 1 from Billing")).toBeNull();
  });

  it("removes a skill by unlinking it", () => {
    state.grants = [readSkills, writeServer];
    renderTab();
    fireEvent.click(screen.getByLabelText("Remove Skill 1 from Billing"));
    expect(state.unlink).toHaveBeenCalledWith(
      { request: { unlinkSkillRequestBody: { linkId: "link-1" } } },
      expect.anything(),
    );
  });

  it("opens the picker on this server, excluding skills it already serves", () => {
    state.grants = [readSkills, writeServer];
    renderTab();
    fireEvent.click(screen.getByText("Add skill"));
    expect(screen.getByTestId("picker").textContent).toBe("toolset-a");
    expect(state.picker).toHaveBeenLastCalledWith(
      expect.objectContaining({
        excludedSkillIds: ["skill-1", "skill-2"],
        target: {
          linkTarget: { kind: "mcp_server", mcpServerId: "toolset-a" },
        },
      }),
    );
  });

  it("leads an empty server with the add action", () => {
    state.grants = [readSkills, writeServer];
    state.links.mockReturnValue(infinite([{ result: { links: [] } }]));
    renderTab();
    expect(screen.getByText("No skills served yet")).toBeDefined();
    expect(screen.getByText("Add skill")).toBeDefined();
  });

  it("offers a retry when a page of links fails", () => {
    state.links.mockReturnValue(
      infinite([{ result: { links: [link("1")] } }], {
        hasNextPage: true,
        error: new Error("boom"),
      }),
    );
    renderTab();
    fireEvent.click(screen.getByText("Retry"));
    expect(state.fetchNextPage).toHaveBeenCalled();
  });

  it("warns when the server is disabled", () => {
    renderTab(<MCPSkillsTab toolset={{ ...toolset, mcpEnabled: false }} />);
    expect(screen.getByText(/This server is disabled/)).toBeDefined();
  });
});

describe("MCPSkillsTabRoute", () => {
  it("redirects to the overview while the flag is off", () => {
    state.flag = { status: "disabled" } as FeatureFlagResult;
    renderTab(<MCPSkillsTabRoute toolset={toolset} />);
    expect(screen.getByTestId("navigate").textContent).toBe(
      "/mcp/toolset-a/overview",
    );
    expect(state.links).not.toHaveBeenCalled();
  });

  it("walls readers without skill read on this project", () => {
    state.grants = [
      { scope: "skill:read", selectors: [{ resourceId: "project-b" }] },
    ];
    renderTab(<MCPSkillsTabRoute toolset={toolset} />);
    expect(screen.getByText("Access restricted")).toBeDefined();
    expect(state.links).not.toHaveBeenCalled();
  });

  it("renders the tab while the flag is on", () => {
    renderTab(<MCPSkillsTabRoute toolset={toolset} />);
    expect(screen.getByText("Skill 1")).toBeDefined();
  });
});
