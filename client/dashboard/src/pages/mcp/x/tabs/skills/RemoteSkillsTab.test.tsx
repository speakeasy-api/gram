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

import { RemoteSkillsTab, RemoteSkillsTabRoute } from "./RemoteSkillsTab";

const remote = {
  id: "remote-a",
  name: "Ticketing",
  remoteMcpServerId: "upstream-a",
};

function link(id: string, overrides: Record<string, unknown> = {}) {
  return {
    id: `link-${id}`,
    skillId: `skill-${id}`,
    skillName: `skill-${id}`,
    skillDisplayName: `Skill ${id}`,
    targetKind: "mcp_server",
    mcpServerId: "remote-a",
    targetName: "Ticketing",
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

function renderTab(ui: ReactNode = <RemoteSkillsTab mcpServer={remote} />) {
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
  selectors: [{ resourceId: "remote-a" }],
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

describe("RemoteSkillsTab", () => {
  it("lists the skills linked to the remote server's row", () => {
    renderTab();
    const names = screen
      .getAllByRole("link")
      .map((element) => element.textContent);
    expect(names).toEqual(["Skill 1", "Skill 2"]);
    expect(state.links).toHaveBeenCalledWith(
      expect.objectContaining({
        mcpServerId: "remote-a",
        targetKind: "mcp_server",
      }),
      undefined,
      expect.anything(),
    );
  });

  it("says linked skills reach agents through gateways, not the server URL", () => {
    renderTab();
    expect(screen.getByText(/own skills pass through/)).toBeDefined();
    expect(screen.getByText(/through gateways that include/)).toBeDefined();
  });

  it("lets writers of the server's own row add and remove skills", () => {
    state.grants = [readSkills, writeServer];
    renderTab();
    fireEvent.click(screen.getByLabelText("Remove Skill 1 from Ticketing"));
    expect(state.unlink).toHaveBeenCalledWith(
      { request: { unlinkSkillRequestBody: { linkId: "link-1" } } },
      expect.anything(),
    );
    fireEvent.click(screen.getByText("Add skill"));
    expect(screen.getByTestId("picker").textContent).toBe("remote-a");
  });

  it("hides add and remove without MCP write on the server", () => {
    renderTab();
    expect(screen.queryByText("Add skill")).toBeNull();
    expect(screen.queryByLabelText("Remove Skill 1 from Ticketing")).toBeNull();
  });
});

describe("RemoteSkillsTabRoute", () => {
  it("redirects to the overview while the flag is off", () => {
    state.flag = { status: "disabled" } as FeatureFlagResult;
    renderTab(
      <RemoteSkillsTabRoute mcpServer={remote} mcpServerSlug="ticketing" />,
    );
    expect(screen.getByTestId("navigate").textContent).toBe(
      "/mcp/ticketing/overview",
    );
    expect(state.links).not.toHaveBeenCalled();
  });

  it("redirects a server that isn't remote-backed", () => {
    renderTab(
      <RemoteSkillsTabRoute
        mcpServer={{ ...remote, remoteMcpServerId: undefined }}
        mcpServerSlug="ticketing"
      />,
    );
    expect(screen.getByTestId("navigate").textContent).toBe(
      "/mcp/ticketing/overview",
    );
    expect(state.links).not.toHaveBeenCalled();
  });

  it("renders the tab while the flag is on", () => {
    renderTab(
      <RemoteSkillsTabRoute mcpServer={remote} mcpServerSlug="ticketing" />,
    );
    expect(screen.getByText("Skill 1")).toBeDefined();
  });
});
