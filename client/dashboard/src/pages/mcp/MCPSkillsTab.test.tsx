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
  canReadOrg: true,
  rbacLoading: false,
  plugins: {
    data: undefined as unknown,
    error: null as unknown,
    isLoading: false,
  },
  servers: {
    data: undefined as unknown,
    error: null as unknown,
    isLoading: false,
    refetch: vi.fn(),
  },
  distributions: vi.fn(),
  fetchNextPage: vi.fn(),
  refetchDistributions: vi.fn(),
  mutate: vi.fn(),
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
  // Org scopes stand for "may list plugins"; everything else reads the grants
  // the way the real hook does, so RequireScope sees project-scoped reads.
  const hasScope = (scope: Scope, resourceId?: string, projectId?: string) =>
    scope.startsWith("org:")
      ? state.canReadOrg
      : original.hasScopeInGrants(
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
vi.mock("@/pages/plugins/usePluginServerQueries", () => ({
  usePluginServerQueries: () => ({
    canReadServers: true,
    serversQuery: state.servers,
    toolsetsQuery: { data: undefined, error: null, isLoading: false },
    endpointsQuery: { data: undefined, error: null, isLoading: false },
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    skills: { detail: { href: (id: string) => `/skills/${id}` } },
    plugins: Object.assign(
      {
        href: () => "/plugins",
        Link: ({ children }: { children: ReactNode }) => (
          <a href="/plugins">{children}</a>
        ),
      },
      { detail: { href: (id: string) => `/plugins/${id}` } },
    ),
    mcp: {
      details: {
        overview: { href: (slug: string) => `/mcp/${slug}/overview` },
      },
    },
  }),
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@gram/client/react-query/plugins.js", () => ({
  usePlugins: () => state.plugins,
}));
vi.mock("@gram/client/react-query/skillDistributions.js", () => ({
  useSkillDistributionsInfinite: state.distributions,
  invalidateAllSkillDistributions: vi.fn(),
}));
vi.mock("@gram/client/react-query/undistributeSkill.js", () => ({
  useUndistributeSkillMutation: () => ({
    mutate: state.mutate,
    isPending: false,
  }),
}));
vi.mock("@/pages/skills/SkillPickerDialog", () => ({
  SkillPickerDialog: (props: { target: { pluginId?: string } }) => {
    state.picker(props);
    return <div data-testid="picker">{props.target.pluginId}</div>;
  },
}));
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  Navigate: ({ to }: { to: string }) => <div data-testid="navigate">{to}</div>,
}));

import { MCPSkillsTab, MCPSkillsTabRoute } from "./MCPSkillsTab";

const toolset = { id: "toolset-a", slug: "toolset-a", name: "Billing" };

const hostedPlugin = {
  id: "plugin-a",
  name: "Finance tools",
  assignmentCount: 2,
  servers: [{ id: "ps-1", displayName: "Billing", toolsetId: "toolset-a" }],
};
const wrapperPlugin = {
  id: "plugin-b",
  name: "Everything",
  assignmentCount: 0,
  servers: [{ id: "ps-2", displayName: "Billing", mcpServerId: "toolset-a" }],
};
const otherPlugin = {
  id: "plugin-c",
  name: "Unrelated",
  assignmentCount: 1,
  servers: [{ id: "ps-3", displayName: "Other", toolsetId: "toolset-z" }],
};
/** Includes the toolset through a second, non-canonical wrapper. */
const secondWrapperPlugin = {
  id: "plugin-d",
  name: "Second wrapper",
  assignmentCount: 1,
  servers: [
    { id: "ps-4", displayName: "Billing (team)", mcpServerId: "wrapper-2" },
  ],
};

function distribution(
  id: string,
  pluginId: string,
  overrides: Record<string, unknown> = {},
) {
  return {
    id,
    skillId: `skill-${id}`,
    pluginId,
    pluginName: "Plugin",
    skillDisplayName: `Skill ${id}`,
    skillName: `skill-${id}`,
    ...overrides,
  };
}

function renderTab(ui: ReactNode = <MCPSkillsTab toolset={toolset} />) {
  return render(
    <TooltipProvider>
      <MemoryRouter>{ui}</MemoryRouter>
    </TooltipProvider>,
  );
}

/** A loaded distributions query with the given pages. */
function distributionsResult(
  distributions: ReturnType<typeof distribution>[],
  overrides: Record<string, unknown> = {},
) {
  const result = {
    data: { pages: [{ result: { distributions } }] },
    hasNextPage: false,
    isFetchingNextPage: false,
    error: null as unknown,
    fetchNextPage: state.fetchNextPage,
    refetch: state.refetchDistributions,
    ...overrides,
  };
  return { ...result, isError: !!result.error };
}

afterEach(cleanup);
beforeEach(() => {
  state.flag = { status: "enabled" };
  state.canReadOrg = true;
  state.rbacLoading = false;
  state.grants = [
    { scope: "skill:read", selectors: [{ resourceId: "project-a" }] },
  ];
  state.plugins = {
    data: { plugins: [hostedPlugin, wrapperPlugin, otherPlugin] },
    error: null,
    isLoading: false,
  };
  state.servers = {
    data: {
      mcpServers: [
        { id: "toolset-a", toolsetId: "toolset-a" },
        { id: "wrapper-2", toolsetId: "toolset-a" },
      ],
    },
    error: null,
    isLoading: false,
    refetch: vi.fn(),
  };
  state.mutate.mockReset();
  state.picker.mockReset();
  state.fetchNextPage.mockReset();
  state.refetchDistributions.mockReset();
  state.distributions.mockReset().mockReturnValue(
    distributionsResult([
      distribution("1", "plugin-a", {
        lastLoadedOverMcpAt: new Date(Date.now() - 5 * 60_000),
      }),
      distribution("2", "plugin-b"),
      distribution("3", "plugin-c"),
    ]),
  );
});

describe("MCPSkillsTab", () => {
  it("lists skills under the plugins that include this server and skips the rest", () => {
    renderTab();
    expect(screen.getByText("Skill 1")).toBeDefined();
    expect(screen.getByText("Skill 2")).toBeDefined();
    expect(screen.queryByText("Skill 3")).toBeNull();
    expect(screen.getByText("Finance tools")).toBeDefined();
    expect(screen.getByText("Everything")).toBeDefined();
    expect(screen.queryByText("Unrelated")).toBeNull();
    // The distributions are the project's, grouped here, not per plugin.
    expect(state.distributions).toHaveBeenCalledWith(
      expect.objectContaining({ gramProject: "project-a", limit: 50 }),
      undefined,
      { throwOnError: false },
    );
  });

  it("serves through a non-canonical wrapper of the toolset", () => {
    state.plugins = {
      data: { plugins: [secondWrapperPlugin, otherPlugin] },
      error: null,
      isLoading: false,
    };
    state.distributions.mockReturnValue(
      distributionsResult([distribution("4", "plugin-d")]),
    );
    renderTab();
    expect(screen.getByText("Second wrapper")).toBeDefined();
    expect(screen.getByText("Skill 4")).toBeDefined();
  });

  it("waits for the server list before deciding membership", () => {
    state.servers = { ...state.servers, data: undefined, isLoading: true };
    renderTab();
    expect(screen.queryByText("Finance tools")).toBeNull();
    expect(screen.queryByText("No plugin includes this server yet")).toBeNull();
  });

  it("offers a retry when the server list fails instead of deciding membership", () => {
    state.servers = {
      ...state.servers,
      data: undefined,
      error: new Error("boom"),
    };
    renderTab();
    expect(screen.getByText("Couldn't load MCP servers")).toBeDefined();
    expect(screen.queryByText("Finance tools")).toBeNull();
    expect(screen.queryByText("No plugin includes this server yet")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.servers.refetch).toHaveBeenCalledTimes(1);
  });

  it("fetches every distributions page so membership covers the project", () => {
    state.distributions.mockReturnValue(
      distributionsResult([distribution("1", "plugin-a")], {
        hasNextPage: true,
      }),
    );
    renderTab();
    expect(state.fetchNextPage).toHaveBeenCalledTimes(1);
  });

  it("stops fetching pages while one is in flight", () => {
    state.distributions.mockReturnValue(
      distributionsResult([distribution("1", "plugin-a")], {
        hasNextPage: true,
        isFetchingNextPage: true,
      }),
    );
    renderTab();
    expect(state.fetchNextPage).not.toHaveBeenCalled();
  });

  it("offers a retry when a later distributions page fails", () => {
    state.distributions.mockReturnValue(
      distributionsResult([distribution("1", "plugin-a")], {
        hasNextPage: true,
        error: new Error("boom"),
      }),
    );
    renderTab();
    expect(screen.getByText("Couldn't load skills")).toBeDefined();
    expect(screen.queryByText("Skill 1")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.fetchNextPage).toHaveBeenCalledTimes(1);
    expect(state.refetchDistributions).not.toHaveBeenCalled();
  });

  it("refetches from the start when the first distributions page fails", () => {
    state.distributions.mockReturnValue(
      distributionsResult([], { data: undefined, error: new Error("boom") }),
    );
    renderTab();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.refetchDistributions).toHaveBeenCalledTimes(1);
    expect(state.fetchNextPage).not.toHaveBeenCalled();
  });

  it("keeps the skeleton up while the grants load", () => {
    state.rbacLoading = true;
    state.canReadOrg = false;
    const { container } = renderTab();
    expect(container.querySelector(".skeleton")).not.toBeNull();
    expect(
      screen.queryByText("You can't see which plugins include this server"),
    ).toBeNull();
  });

  it("explains itself to a reader who cannot list plugins", () => {
    state.canReadOrg = false;
    renderTab();
    expect(
      screen.getByText("You can't see which plugins include this server"),
    ).toBeDefined();
    expect(screen.queryByText("Finance tools")).toBeNull();
  });

  it("shows when a skill was last loaded over MCP and warns about unassigned plugins", () => {
    renderTab();
    expect(screen.getAllByText("Never")).toHaveLength(1);
    expect(screen.getByText(/5 minutes ago/)).toBeDefined();
    expect(screen.getByText(/Not assigned to anyone yet/)).toBeDefined();
  });

  it("hides add and remove controls without plugin write", () => {
    renderTab();
    expect(screen.queryByRole("button", { name: /Add skill/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /^Remove/ })).toBeNull();
  });

  it("removes a served skill by undistributing it from its plugin", () => {
    state.grants.push({
      scope: "plugin:write",
      selectors: [{ resourceKind: "project", resourceId: "project-a" }],
    });
    renderTab();
    fireEvent.click(
      screen.getByRole("button", { name: "Remove Skill 1 from Plugin" }),
    );
    expect(state.mutate).toHaveBeenCalledWith(
      {
        request: {
          undistributeSkillRequestBody: {
            id: "skill-1",
            pluginId: "plugin-a",
          },
        },
      },
      expect.any(Object),
    );
  });

  it("opens the picker for the only plugin that includes the server", () => {
    state.grants.push({
      scope: "plugin:write",
      selectors: [{ resourceKind: "project", resourceId: "project-a" }],
    });
    state.plugins = {
      data: { plugins: [hostedPlugin, otherPlugin] },
      error: null,
      isLoading: false,
    };
    renderTab();
    fireEvent.click(screen.getByRole("button", { name: "Add skill" }));
    expect(screen.getByTestId("picker").textContent).toBe("plugin-a");
    expect(state.picker).toHaveBeenLastCalledWith(
      expect.objectContaining({
        excludedSkillIds: ["skill-1"],
        target: { pluginId: "plugin-a" },
      }),
    );
  });

  it("points at the plugins page when no plugin includes the server", () => {
    state.plugins = {
      data: { plugins: [otherPlugin] },
      error: null,
      isLoading: false,
    };
    renderTab();
    expect(
      screen.getByText("No plugin includes this server yet"),
    ).toBeDefined();
    expect(
      screen.getByText("Go to plugins").closest("a")?.getAttribute("href"),
    ).toBe("/plugins");
  });

  it("says so when a plugin includes a server the reader cannot resolve", () => {
    // The server list is filtered by the reader's grants, so a wrapper of
    // this toolset they cannot view is unresolved, not proof of absence.
    state.plugins = {
      data: {
        plugins: [
          otherPlugin,
          {
            id: "plugin-e",
            name: "Hidden wrapper",
            assignmentCount: 1,
            servers: [
              { id: "ps-5", displayName: "Hidden", mcpServerId: "wrapper-9" },
            ],
          },
        ],
      },
      error: null,
      isLoading: false,
    };
    renderTab();
    expect(
      screen.getByText("No plugin you can see includes this server"),
    ).toBeDefined();
    expect(screen.queryByText("No plugin includes this server yet")).toBeNull();
    expect(
      screen.getByText("Go to plugins").closest("a")?.getAttribute("href"),
    ).toBe("/plugins");
  });

  it("names the plugin to distribute to when nothing is served yet", () => {
    state.plugins = {
      data: { plugins: [hostedPlugin] },
      error: null,
      isLoading: false,
    };
    state.distributions.mockReturnValue(distributionsResult([]));
    renderTab();
    expect(screen.getByText("No skills served yet")).toBeDefined();
    expect(
      screen.getByText(/Distribute a skill to Finance tools/),
    ).toBeDefined();
  });
});

describe("MCPSkillsTabRoute", () => {
  it("redirects to the overview while the flag is off, before any scope check", () => {
    state.flag = { status: "disabled" };
    state.grants = [];
    renderTab(<MCPSkillsTabRoute toolset={toolset} />);
    expect(screen.getByTestId("navigate").textContent).toBe(
      "/mcp/toolset-a/overview",
    );
    expect(screen.queryByText("Access restricted")).toBeNull();
  });

  it("opens the tab for a skill reader of this project", () => {
    renderTab(<MCPSkillsTabRoute toolset={toolset} />);
    expect(screen.getByText("Finance tools")).toBeDefined();
  });

  it("walls a reader whose skill access is for another project", () => {
    state.grants = [
      { scope: "skill:read", selectors: [{ resourceId: "project-b" }] },
    ];
    renderTab(<MCPSkillsTabRoute toolset={toolset} />);
    expect(screen.getByText("Access restricted")).toBeDefined();
    expect(screen.queryByText("Finance tools")).toBeNull();
  });
});
