import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  scopes: [] as string[],
  /**
   * When set, the mcp:write grant is selector-scoped to these server ids:
   * `hasScope("mcp:write")` with no resource still passes (an existential
   * check, as in useRBAC), but `hasScope("mcp:write", id)` only for listed ids.
   */
  mcpWriteResourceIds: null as string[] | null,
  inProject: true,
  toolsets: [] as unknown[],
  mcpServers: [] as unknown[],
  catalogServers: [] as unknown[],
  publishStatus: undefined as unknown,
  members: [] as unknown[],
  actions: [] as unknown[],
  recents: [] as unknown[],
  navigate: vi.fn(),
  goToPlugins: vi.fn(),
  goToToolsetDetails: vi.fn(),
  goToMcpServerOverview: vi.fn(),
  goToCatalogDetail: vi.fn(),
  updateMcpServer: vi.fn(),
  publishPlugins: vi.fn(),
  invalidateMcpServerQueries: vi.fn().mockResolvedValue(undefined),
  invalidateAllPublishStatus: vi.fn().mockResolvedValue(undefined),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  // Every SDK list hook records the options it was called with so the test can
  // prove nothing fetches while the palette is closed.
  calls: {} as Record<string, unknown[]>,
}));

function record(name: string, args: unknown[]) {
  (mocks.calls[name] ??= []).push(args);
}

vi.mock("@gram/client/react-query/listToolsets.js", () => ({
  useListToolsets: (...args: unknown[]) => {
    record("useListToolsets", args);
    return { data: { toolsets: mocks.toolsets } };
  },
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: (...args: unknown[]) => {
    record("useMcpServers", args);
    return { data: { mcpServers: mocks.mcpServers } };
  },
}));
vi.mock("@gram/client/react-query/listMCPCatalog.js", () => ({
  useListMCPCatalog: (...args: unknown[]) => {
    record("useListMCPCatalog", args);
    return { data: { servers: mocks.catalogServers } };
  },
}));
vi.mock("@gram/client/react-query/publishStatus.js", () => ({
  usePublishStatus: (...args: unknown[]) => {
    record("usePublishStatus", args);
    return { data: mocks.publishStatus };
  },
  invalidateAllPublishStatus: mocks.invalidateAllPublishStatus,
}));
vi.mock("@gram/client/react-query/publishPlugins.js", () => ({
  usePublishPluginsMutation: () => ({ mutateAsync: mocks.publishPlugins }),
}));
vi.mock("@gram/client/react-query/updateMcpServer.js", () => ({
  useUpdateMcpServerMutation: () => ({ mutateAsync: mocks.updateMcpServer }),
}));
vi.mock("@gram/client/react-query/plugins.js", () => ({
  usePlugins: (...args: unknown[]) => {
    record("usePlugins", args);
    return { data: { plugins: [] } };
  },
}));
vi.mock("@gram/client/react-query/assistantsList.js", () => ({
  useAssistantsList: (...args: unknown[]) => {
    record("useAssistantsList", args);
    return { data: { assistants: [] } };
  },
}));
vi.mock("@gram/client/react-query/listEnvironments.js", () => ({
  useListEnvironments: (...args: unknown[]) => {
    record("useListEnvironments", args);
    return { data: { environments: [] } };
  },
}));
vi.mock("@gram/client/react-query/latestDeployment.js", () => ({
  useLatestDeployment: (...args: unknown[]) => {
    record("useLatestDeployment", args);
    return { data: { deployment: undefined } };
  },
}));
vi.mock("@gram/client/react-query/listDeployments.js", () => ({
  useListDeployments: (...args: unknown[]) => {
    record("useListDeployments", args);
    return { data: { items: [] } };
  },
}));
vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  useRiskListPolicies: (...args: unknown[]) => {
    record("useRiskListPolicies", args);
    return { data: { policies: [] } };
  },
}));
vi.mock("@gram/client/react-query/riskListCustomDetectionRules.js", () => ({
  useRiskListCustomDetectionRules: (...args: unknown[]) => {
    record("useRiskListCustomDetectionRules", args);
    return { data: { rules: [] } };
  },
}));
vi.mock("@gram/client/react-query/listMcpApprovalRequests.js", () => ({
  useListMcpApprovalRequests: (...args: unknown[]) => {
    record("useListMcpApprovalRequests", args);
    return { data: { requests: [] } };
  },
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: (...args: unknown[]) => {
    record("useMembers", args);
    return { data: { members: mocks.members } };
  },
}));

vi.mock("@/lib/mcp-server-visibility", () => ({
  mcpServerVisibilityUpdateForm: (
    server: { id: string },
    visibility: string,
  ) => ({ id: server.id, visibility }),
  mcpServerVisibilityToast: (visibility: string) =>
    visibility === "disabled" ? "MCP server disabled" : "MCP server enabled",
  invalidateMcpServerQueries: mocks.invalidateMcpServerQueries,
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));
vi.mock("sonner", () => ({
  toast: { success: mocks.toastSuccess, error: mocks.toastError },
}));
vi.mock("react-router", () => ({
  useNavigate: () => mocks.navigate,
  useLocation: () => ({ search: "" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({
    orgSlug: "acme",
    projectSlug: mocks.inProject ? "default" : undefined,
  }),
  useProjectSlugForRequests: () => "default",
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasAnyScope: (scopes: string[]) =>
      scopes.some((scope) => mocks.scopes.includes(scope)),
    hasScope: (scope: string, resourceId?: string) => {
      if (!mocks.scopes.includes(scope)) return false;
      if (scope !== "mcp:write" || !mocks.mcpWriteResourceIds) return true;
      return !resourceId || mocks.mcpWriteResourceIds.includes(resourceId);
    },
  }),
}));
vi.mock("@/contexts/CommandPalette", () => ({
  useCommandPalette: () => ({ actions: mocks.actions }),
}));
vi.mock("../recentlyVisited", () => ({
  useRecentlyVisited: () => mocks.recents,
  getRecentLabelOverride: () => undefined,
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    plugins: {
      goTo: mocks.goToPlugins,
      href: () => "/acme/projects/default/plugins",
    },
    mcp: {
      goTo: vi.fn(),
      details: { goTo: mocks.goToToolsetDetails },
      x: { overview: { goTo: mocks.goToMcpServerOverview } },
      catalog: { detail: { goTo: mocks.goToCatalogDetail } },
    },
    assistants: { detail: { goTo: vi.fn() } },
    environments: { environment: { goTo: vi.fn() } },
    deployments: { deployment: { goTo: vi.fn() } },
    policyCenter: { href: () => "/acme/projects/default/policy-center" },
    shadowMCP: {
      href: () => "/acme/projects/default/shadow-mcp",
      detail: {
        href: (slug: string) => `/acme/projects/default/shadow-mcp/${slug}`,
      },
    },
    identities: {
      detail: { overview: { href: (urn: string) => `/identities/${urn}` } },
    },
  }),
}));

import { useLauncherCandidates } from "./index";
import type { LauncherCandidate } from "./types";

const REMOTE = { remoteMcpServerId: "remote-1" };

function mcpServer(
  name: string,
  slug: string,
  visibility: "private" | "public" | "disabled",
) {
  return { id: `server-${slug}`, name, slug, visibility, ...REMOTE };
}

function renderCandidates(
  overrides: Partial<Parameters<typeof useLauncherCandidates>[0]> = {},
) {
  return renderHook(() =>
    useLauncherCandidates({
      enabled: true,
      inProject: mocks.inProject,
      recentsUserId: "user_1",
      orgSlug: "acme",
      projectSlug: mocks.inProject ? "default" : undefined,
      ...overrides,
    }),
  );
}

function byKind(list: LauncherCandidate[], kind: LauncherCandidate["kind"]) {
  return list.filter((c) => c.kind === kind);
}

function catalogServer(title: string | undefined, registrySpecifier: string) {
  return { title, registrySpecifier, registryId: "registry-1" };
}

beforeEach(() => {
  mocks.scopes = [];
  mocks.mcpWriteResourceIds = null;
  mocks.inProject = true;
  mocks.toolsets = [];
  mocks.mcpServers = [];
  mocks.catalogServers = [];
  mocks.publishStatus = undefined;
  mocks.members = [];
  mocks.actions = [];
  mocks.recents = [];
  mocks.calls = {};
  vi.clearAllMocks();
  mocks.invalidateMcpServerQueries.mockResolvedValue(undefined);
  mocks.invalidateAllPublishStatus.mockResolvedValue(undefined);
});
afterEach(cleanup);

describe("useMcpServerCandidates", () => {
  it("reads visibility into detail and offers only open without mcp:write", () => {
    mocks.mcpServers = [
      mcpServer("Slack", "slack-a1", "private"),
      mcpServer("Jira", "jira-b2", "disabled"),
    ];
    const { result } = renderCandidates();
    const [slack, jira] = byKind(result.current, "mcp_server");

    expect(slack?.detail).toBe("MCP server · enabled");
    expect(jira?.detail).toBe("MCP server · disabled");
    expect(slack?.verbs).toEqual(["open"]);
    expect(jira?.verbs).toEqual(["open"]);
  });

  it("attaches disable to enabled servers and enable to disabled ones for writers", () => {
    mocks.scopes = ["mcp:write"];
    mocks.mcpServers = [
      mcpServer("Slack", "slack-a1", "private"),
      mcpServer("Jira", "jira-b2", "disabled"),
    ];
    const { result } = renderCandidates();
    const [slack, jira] = byKind(result.current, "mcp_server");

    expect(slack?.verbs).toEqual(["open", "disable"]);
    expect(jira?.verbs).toEqual(["open", "enable"]);
  });

  // The page gates the same control per server (`RequireScope resourceId`),
  // so a grant scoped to one server must not attach verbs to every server.
  it("attaches enable/disable only to servers the mcp:write grant covers", () => {
    mocks.scopes = ["mcp:write"];
    mocks.mcpWriteResourceIds = ["server-slack-a1"];
    mocks.mcpServers = [
      mcpServer("Slack", "slack-a1", "private"),
      mcpServer("Jira", "jira-b2", "disabled"),
    ];
    const { result } = renderCandidates();
    const [slack, jira] = byKind(result.current, "mcp_server");

    expect(slack?.verbs).toEqual(["open", "disable"]);
    expect(jira?.verbs).toEqual(["open"]);
  });

  // A toolset row with no mcp_servers record has no visibility to flip, so it
  // is open-only even for a writer.
  it("keeps toolset-backed rows without a linked mcp_servers row open-only", () => {
    mocks.scopes = ["mcp:write"];
    mocks.toolsets = [{ id: "toolset-1", name: "Hosted", slug: "hosted" }];
    const { result } = renderCandidates();
    const [hosted] = byKind(result.current, "mcp_server");

    expect(hosted?.title).toBe("Hosted");
    expect(hosted?.detail).toBe("MCP server · enabled");
    expect(hosted?.verbs).toEqual(["open"]);
    expect(hosted?.keywords).toContain("hosted");
  });

  // Hosted servers carry their visibility on the toolset-backed mcp_servers
  // row, which is hidden from the list to avoid doubling; the toolset
  // candidate must still read that row's state and flip it.
  it("reads a hosted server's visibility from its linked mcp_servers row", async () => {
    mocks.scopes = ["mcp:write"];
    mocks.toolsets = [{ id: "toolset-1", name: "Hosted", slug: "hosted" }];
    mocks.mcpServers = [
      {
        id: "server-hosted",
        name: "Hosted",
        slug: "hosted-x1",
        toolsetId: "toolset-1",
        visibility: "disabled",
      },
    ];
    mocks.updateMcpServer.mockResolvedValue({});
    const { result } = renderCandidates();
    const servers = byKind(result.current, "mcp_server");
    expect(servers).toHaveLength(1);
    const [hosted] = servers;

    expect(hosted?.detail).toBe("MCP server · disabled");
    expect(hosted?.verbs).toEqual(["open", "enable"]);

    await act(async () => {
      await hosted?.run("open");
    });
    expect(mocks.goToToolsetDetails).toHaveBeenCalledWith("hosted");

    await act(async () => {
      await hosted?.run("enable");
    });
    expect(mocks.updateMcpServer).toHaveBeenCalledWith({
      request: {
        updateMcpServerForm: { id: "server-hosted", visibility: "private" },
      },
    });
    expect(mocks.toastSuccess).toHaveBeenCalledWith("MCP server enabled");
  });

  it("scopes a hosted server's verbs to the grant covering its linked row", () => {
    mocks.scopes = ["mcp:write"];
    mocks.mcpWriteResourceIds = ["server-other"];
    mocks.toolsets = [{ id: "toolset-1", name: "Hosted", slug: "hosted" }];
    mocks.mcpServers = [
      {
        id: "server-hosted",
        name: "Hosted",
        slug: "hosted-x1",
        toolsetId: "toolset-1",
        visibility: "private",
      },
    ];
    const { result } = renderCandidates();
    const [hosted] = byKind(result.current, "mcp_server");

    expect(hosted?.detail).toBe("MCP server · enabled");
    expect(hosted?.verbs).toEqual(["open"]);
  });

  it("omits toolset-backed mcp_servers rows so hosted servers aren't doubled", () => {
    mocks.toolsets = [{ id: "toolset-1", name: "Hosted", slug: "hosted" }];
    mocks.mcpServers = [
      {
        id: "server-dupe",
        name: "Hosted",
        slug: "hosted-dupe",
        toolsetId: "toolset-1",
        visibility: "private",
      },
    ];
    const { result } = renderCandidates();

    expect(byKind(result.current, "mcp_server")).toHaveLength(1);
  });

  it("navigates on open and flips visibility on disable", async () => {
    mocks.scopes = ["mcp:write"];
    mocks.mcpServers = [mcpServer("Slack", "slack-a1", "private")];
    mocks.updateMcpServer.mockResolvedValue({});
    const { result } = renderCandidates();
    const [slack] = byKind(result.current, "mcp_server");

    await act(async () => {
      await slack?.run("open");
    });
    expect(mocks.goToMcpServerOverview).toHaveBeenCalledWith("slack-a1");

    await act(async () => {
      await slack?.run("disable");
    });
    expect(mocks.updateMcpServer).toHaveBeenCalledWith({
      request: {
        updateMcpServerForm: { id: "server-slack-a1", visibility: "disabled" },
      },
    });
    expect(mocks.invalidateMcpServerQueries).toHaveBeenCalled();
    expect(mocks.toastSuccess).toHaveBeenCalledWith("MCP server disabled");
  });

  it("toasts and rethrows when the visibility update fails", async () => {
    mocks.scopes = ["mcp:write"];
    mocks.mcpServers = [mcpServer("Jira", "jira-b2", "disabled")];
    mocks.updateMcpServer.mockRejectedValue(new Error("boom"));
    const { result } = renderCandidates();
    const [jira] = byKind(result.current, "mcp_server");

    await expect(jira?.run("enable")).rejects.toThrow("boom");
    expect(mocks.toastError).toHaveBeenCalledWith("boom");
    expect(mocks.toastSuccess).not.toHaveBeenCalled();
  });
});

describe("useCatalogCandidates", () => {
  beforeEach(() => {
    mocks.scopes = ["project:read"];
  });

  it("lists catalog entries with the specifier as detail and keyword", () => {
    mocks.catalogServers = [
      catalogServer("Datadog", "com.datadoghq/datadog"),
      catalogServer("Notion", "com.notion/notion"),
    ];
    const { result } = renderCandidates();
    const [datadog, notion] = byKind(result.current, "catalog");

    expect(datadog?.id).toBe("catalog:com.datadoghq/datadog");
    expect(datadog?.title).toBe("Datadog");
    expect(datadog?.detail).toBe("Catalog entry · com.datadoghq/datadog");
    expect(datadog?.keywords).toContain("com.datadoghq/datadog");
    expect(datadog?.verbs).toEqual(["open"]);
    expect(datadog?.group).toBe("MCP Catalog");
    expect(notion?.title).toBe("Notion");
  });

  // The distinction the group exists to make: a name a project already runs
  // and offers in the catalog yields one row of each kind, and what you
  // already run reads first.
  it("keeps catalog entries apart from, and after, servers the project runs", () => {
    mocks.mcpServers = [mcpServer("Datadog", "datadog-a1b2c3", "private")];
    mocks.catalogServers = [catalogServer("Datadog", "com.datadoghq/datadog")];
    const { result } = renderCandidates();
    const datadogs = result.current.filter((c) => c.title === "Datadog");

    expect(datadogs.map((c) => c.kind)).toEqual(["mcp_server", "catalog"]);
  });

  it("opens the catalog entry's detail page on open", () => {
    mocks.catalogServers = [catalogServer("Datadog", "com.datadoghq/datadog")];
    const { result } = renderCandidates();
    const [datadog] = byKind(result.current, "catalog");

    void datadog?.run("open");
    expect(mocks.goToCatalogDetail).toHaveBeenCalledWith(
      encodeURIComponent("com.datadoghq/datadog"),
    );
    expect(mocks.goToMcpServerOverview).not.toHaveBeenCalled();
  });

  // Standing in as the title, the specifier is not repeated in the detail.
  it("falls back to the specifier when the registry publishes no title", () => {
    mocks.catalogServers = [catalogServer(undefined, "io.github.acme/widgets")];
    const { result } = renderCandidates();
    const [widgets] = byKind(result.current, "catalog");

    expect(widgets?.title).toBe("io.github.acme/widgets");
    expect(widgets?.detail).toBe("Catalog entry");
  });

  // The detail route is addressed by specifier alone and resolves the first
  // match, so a second registry publishing the same server would otherwise add
  // an identical-looking row that leads to the very same page.
  it("offers one row per specifier when two registries publish the same server", () => {
    mocks.catalogServers = [
      catalogServer("Datadog", "com.datadoghq/datadog"),
      {
        ...catalogServer("Datadog", "com.datadoghq/datadog"),
        registryId: "registry-2",
      },
    ];
    const { result } = renderCandidates();

    expect(byKind(result.current, "catalog")).toHaveLength(1);
  });

  it("stays hidden, and does not fetch, without project:read", () => {
    mocks.scopes = [];
    mocks.catalogServers = [catalogServer("Datadog", "com.datadoghq/datadog")];
    const { result } = renderCandidates();

    expect(byKind(result.current, "catalog")).toHaveLength(0);
    for (const args of mocks.calls["useListMCPCatalog"] ?? []) {
      const options = (args as unknown[])[2] as { enabled?: boolean };
      expect(options?.enabled).toBe(false);
    }
  });

  // listCatalog requires project:read. Fetching for a writer who lacks it
  // would fire a request the backend refuses.
  it("stays hidden for a writer without project:read", () => {
    mocks.scopes = ["mcp:write"];
    mocks.catalogServers = [catalogServer("Datadog", "com.datadoghq/datadog")];
    const { result } = renderCandidates();

    expect(byKind(result.current, "catalog")).toHaveLength(0);
  });

  it("is absent outside a project", () => {
    mocks.inProject = false;
    mocks.catalogServers = [catalogServer("Datadog", "com.datadoghq/datadog")];
    const { result } = renderCandidates();

    expect(byKind(result.current, "catalog")).toHaveLength(0);
  });
});

describe("useMarketplaceCandidate", () => {
  it("is absent until the publish status resolves", () => {
    const { result } = renderCandidates();
    expect(byKind(result.current, "marketplace")).toHaveLength(0);
  });

  it("is absent outside a project", () => {
    mocks.inProject = false;
    mocks.publishStatus = { configured: true, connected: true, upToDate: true };
    const { result } = renderCandidates();
    expect(byKind(result.current, "marketplace")).toHaveLength(0);
  });

  it("describes unpublished changes and offers publish to org admins", () => {
    mocks.scopes = ["org:admin"];
    mocks.publishStatus = {
      configured: true,
      connected: true,
      upToDate: false,
    };
    const { result } = renderCandidates();
    const [marketplace] = byKind(result.current, "marketplace");

    expect(marketplace?.id).toBe("marketplace");
    expect(marketplace?.title).toBe("Plugin marketplace");
    expect(marketplace?.detail).toBe(
      "Plugin marketplace · unpublished changes",
    );
    expect(marketplace?.verbs).toEqual(["open", "publish"]);
  });

  it("stays open-only without org:admin", () => {
    mocks.publishStatus = { configured: true, connected: true, upToDate: true };
    const { result } = renderCandidates();
    const [marketplace] = byKind(result.current, "marketplace");

    expect(marketplace?.detail).toBe("Plugin marketplace · up to date");
    expect(marketplace?.verbs).toEqual(["open"]);
  });

  it("stays open-only when not connected, even for admins", () => {
    mocks.scopes = ["org:admin"];
    mocks.publishStatus = { configured: true, connected: false };
    const { result } = renderCandidates();
    const [marketplace] = byKind(result.current, "marketplace");

    expect(marketplace?.detail).toBe("Plugin marketplace · not connected");
    expect(marketplace?.verbs).toEqual(["open"]);
  });

  it("opens the plugins page and publishes with no collaborators", async () => {
    mocks.scopes = ["org:admin"];
    mocks.publishStatus = {
      configured: true,
      connected: true,
      upToDate: false,
    };
    mocks.publishPlugins.mockResolvedValue({ repoUrl: "https://example.test" });
    const { result } = renderCandidates();
    const [marketplace] = byKind(result.current, "marketplace");

    await act(async () => {
      await marketplace?.run("open");
    });
    expect(mocks.goToPlugins).toHaveBeenCalled();

    await act(async () => {
      await marketplace?.run("publish");
    });
    expect(mocks.publishPlugins).toHaveBeenCalledWith({
      security: { sessionHeaderGramSession: "" },
      request: { publishPluginsRequestBody: { githubUsernames: [] } },
    });
    expect(mocks.invalidateAllPublishStatus).toHaveBeenCalled();
    expect(mocks.toastSuccess).toHaveBeenCalledWith(
      "Plugins published to GitHub",
    );
  });

  it("toasts and rethrows when publishing fails", async () => {
    mocks.scopes = ["org:admin"];
    mocks.publishStatus = {
      configured: true,
      connected: true,
      upToDate: false,
    };
    mocks.publishPlugins.mockRejectedValue(new Error("nope"));
    const { result } = renderCandidates();
    const [marketplace] = byKind(result.current, "marketplace");

    await expect(marketplace?.run("publish")).rejects.toThrow("nope");
    expect(mocks.toastError).toHaveBeenCalledWith(
      "Failed to publish plugins to GitHub",
    );
  });
});

describe("usePersonCandidates", () => {
  it("yields person candidates for org readers", () => {
    mocks.scopes = ["org:read"];
    mocks.members = [
      { id: "m1", name: "Ada", email: "ada@example.test", roleIds: ["admin"] },
    ];
    const { result } = renderCandidates();
    const [ada] = byKind(result.current, "person");

    expect(ada?.kind).toBe("person");
    expect(ada?.title).toBe("Ada");
    expect(ada?.detail).toBe("Member · admin");
    expect(ada?.keywords).toContain("ada@example.test");
    expect(ada?.verbs).toEqual(["open"]);
  });

  it("yields no person candidates without an org scope", () => {
    mocks.members = [
      { id: "m1", name: "Ada", email: "ada@example.test", roleIds: [] },
    ];
    const { result } = renderCandidates();
    expect(byKind(result.current, "person")).toHaveLength(0);
  });
});

describe("useActionCandidates", () => {
  it("maps the action group to the detail string", () => {
    const onSelect = vi.fn();
    mocks.actions = [
      {
        id: "nav-org-settings",
        label: "Settings",
        group: "Organization",
        onSelect,
      },
      {
        id: "nav-page-mcp",
        label: "MCP",
        group: "Pages",
        onSelect,
        stage: "beta",
      },
      { id: "tool-1", label: "Run tool", group: "Tool Actions", onSelect },
    ];
    const { result } = renderCandidates();
    const pages = byKind(result.current, "page");

    expect(pages.map((p) => p.detail)).toEqual([
      "Organization page",
      "Page",
      "Tool action",
    ]);
    expect(pages[0]?.id).toBe("action:nav-org-settings");
    expect(pages[1]?.stage).toBe("beta");

    void pages[0]?.run("open");
    expect(onSelect).toHaveBeenCalledTimes(1);
  });
});

describe("useRecentCandidates", () => {
  it("adapts recents into open-only candidates that navigate", () => {
    mocks.recents = [
      {
        label: "Slack",
        href: "/acme/projects/default/mcp/slack",
        icon: "network",
        visitedAt: 1,
      },
    ];
    const { result } = renderCandidates();
    const [recent] = byKind(result.current, "recent");

    expect(recent?.id).toBe("recent:/acme/projects/default/mcp/slack");
    expect(recent?.detail).toBe("Recently visited");
    expect(recent?.verbs).toEqual(["open"]);
    void recent?.run("open");
    expect(mocks.navigate).toHaveBeenCalledWith(
      "/acme/projects/default/mcp/slack",
    );
    expect(recent?.fuzzyOnly).toBeUndefined();
  });

  // Identity pages register the person's display name (or their email) as the
  // recent's label. That label must rank by fuzzy alone, like People, so it
  // never reaches the intent service.
  it("marks identity-page recents fuzzy-only so names never leave the tenant", () => {
    mocks.recents = [
      {
        label: "ada@example.test",
        href: "/acme/projects/default/identities/dXNlcjptMQ/overview",
        icon: "user",
        visitedAt: 2,
      },
      {
        label: "Identities",
        href: "/acme/projects/default/identities",
        icon: "users",
        visitedAt: 1,
      },
    ];
    const { result } = renderCandidates();
    const [person, index] = byKind(result.current, "recent");

    expect(person?.title).toBe("ada@example.test");
    expect(person?.fuzzyOnly).toBe(true);
    expect(index?.fuzzyOnly).toBeUndefined();
  });
});

describe("useLauncherCandidates", () => {
  it("orders sources: actions, recents, marketplace, mcp servers, catalog, …, people", () => {
    mocks.scopes = ["org:read", "org:admin", "mcp:write", "project:read"];
    mocks.actions = [
      { id: "a", label: "A", group: "Pages", onSelect: vi.fn() },
    ];
    mocks.recents = [{ label: "R", href: "/r", visitedAt: 1 }];
    mocks.publishStatus = { configured: true, connected: true, upToDate: true };
    mocks.mcpServers = [mcpServer("Slack", "slack-a1", "private")];
    mocks.catalogServers = [catalogServer("Datadog", "com.datadoghq/datadog")];
    mocks.members = [{ id: "m1", name: "Ada", email: "a@x", roleIds: [] }];
    const { result } = renderCandidates();

    // Built-in detection rules are static, so an admin always has `rule` rows.
    expect([...new Set(result.current.map((c) => c.kind))]).toEqual([
      "page",
      "recent",
      "marketplace",
      "mcp_server",
      "catalog",
      "rule",
      "person",
    ]);
  });

  it("passes enabled:false to every SDK query while the palette is closed", () => {
    mocks.scopes = ["org:read", "org:admin", "project:read"];
    renderCandidates({ enabled: false });

    const names = Object.keys(mocks.calls);
    expect(names).toEqual(
      expect.arrayContaining([
        "useListToolsets",
        "useMcpServers",
        "useListMCPCatalog",
        "usePublishStatus",
        "usePlugins",
        "useAssistantsList",
        "useListEnvironments",
        "useLatestDeployment",
        "useListDeployments",
        "useRiskListPolicies",
        "useRiskListCustomDetectionRules",
        "useListMcpApprovalRequests",
        "useMembers",
      ]),
    );
    for (const name of names) {
      for (const args of mocks.calls[name] ?? []) {
        const options = (args as unknown[])[2] as { enabled?: boolean };
        expect(options?.enabled, name).toBe(false);
      }
    }
  });

  it("does not fetch admin-only groups for non-admins even when open", () => {
    renderCandidates();

    for (const name of [
      "useRiskListPolicies",
      "useRiskListCustomDetectionRules",
      "useListMcpApprovalRequests",
      "useMembers",
    ]) {
      for (const args of mocks.calls[name] ?? []) {
        const options = (args as unknown[])[2] as { enabled?: boolean };
        expect(options?.enabled, name).toBe(false);
      }
    }
  });
});
