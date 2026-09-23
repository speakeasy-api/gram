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
  deployment: undefined as unknown,
  rules: [] as unknown[],
  members: [] as unknown[],
  actions: [] as unknown[],
  recents: [] as unknown[],
  recentsArgs: [] as unknown[][],
  navigate: vi.fn(),
  goToPlugins: vi.fn(),
  goToToolsetDetails: vi.fn(),
  goToMcpServerOverview: vi.fn(),
  goToCatalogDetail: vi.fn(),
  goToSourceDetail: vi.fn(),
  goToSources: vi.fn(),
  updateMcpServer: vi.fn(),
  /** Latest server rows by id, as the pre-update refetch returns them. */
  latestServers: {} as Record<string, unknown>,
  fetchQuery: vi.fn(),
  flags: [] as string[],
  policies: [] as unknown[],
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
  invalidateAllMcpServers: vi.fn(),
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
vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  buildGetMcpServerQuery: (_client: unknown, request: { id: string }) => ({
    queryKey: ["getMcpServer", request.id],
  }),
  invalidateAllGetMcpServer: vi.fn(),
}));
vi.mock("@gram/client/react-query/plugins.js", () => ({
  usePlugins: (...args: unknown[]) => {
    record("usePlugins", args);
    return { data: { plugins: [] } };
  },
  invalidateAllPlugins: vi.fn(),
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
    return { data: { deployment: mocks.deployment } };
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
    return { data: { policies: mocks.policies } };
  },
}));
vi.mock("@gram/client/react-query/riskListCustomDetectionRules.js", () => ({
  useRiskListCustomDetectionRules: (...args: unknown[]) => {
    record("useRiskListCustomDetectionRules", args);
    return { data: { rules: mocks.rules } };
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

// The real form builder and toast copy run here: the update endpoint replaces
// the whole record, so the tests must see every carried-over field, not a
// stand-in shape the real code never sends.
vi.mock("@/lib/mcp-server-visibility", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/mcp-server-visibility")>()),
  invalidateMcpServerQueries: mocks.invalidateMcpServerQueries,
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ fetchQuery: mocks.fetchQuery }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({
    isFeatureEnabled: (flag: string) => mocks.flags.includes(flag),
  }),
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
  useSdkClient: () => ({}),
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
  useRecentlyVisited: (...args: unknown[]) => {
    mocks.recentsArgs.push(args);
    return mocks.recents;
  },
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
      sources: {
        goTo: mocks.goToSources,
        detail: { goTo: mocks.goToSourceDetail },
      },
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
  return {
    id: `server-${slug}`,
    name,
    slug,
    visibility,
    environmentId: "env-1",
    ...REMOTE,
  };
}

const HOSTED_ROW = {
  id: "server-hosted",
  name: "Hosted",
  slug: "hosted-x1",
  toolsetId: "toolset-1",
  environmentId: "env-1",
  toolVariationsGroupId: "tvg-1",
};

/** The options object every SDK list hook in this file receives. */
function optionsOf(args: unknown): {
  enabled?: boolean;
  throwOnError?: unknown;
} {
  return ((args as unknown[])[2] ?? {}) as {
    enabled?: boolean;
    throwOnError?: unknown;
  };
}

/** The request object (first argument) every SDK list hook receives. */
function requestOf(args: unknown): { gramProject?: string } | undefined {
  return (args as unknown[])[0] as { gramProject?: string } | undefined;
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
  mocks.deployment = undefined;
  mocks.rules = [];
  mocks.members = [];
  mocks.actions = [];
  mocks.recents = [];
  mocks.recentsArgs = [];
  mocks.calls = {};
  mocks.latestServers = {};
  mocks.flags = [];
  mocks.policies = [];
  vi.clearAllMocks();
  // The pre-update refetch answers with the freshest row known for the id:
  // an explicit "latest" fixture, else the cached list row.
  mocks.fetchQuery.mockImplementation(
    async ({ queryKey }: { queryKey: [string, string] }) =>
      mocks.latestServers[queryKey[1]] ??
      (mocks.mcpServers as Array<{ id: string }>).find(
        (server) => server.id === queryKey[1],
      ),
  );
  mocks.invalidateMcpServerQueries.mockResolvedValue(undefined);
  mocks.invalidateAllPublishStatus.mockResolvedValue(undefined);
});
afterEach(cleanup);

describe("useMcpServerCandidates", () => {
  // The MCP page opens for mcp:read or mcp:write; every test here holds the
  // reader scope and adds mcp:write where the verbs need it.
  beforeEach(() => {
    mocks.scopes = ["mcp:read"];
  });

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
    mocks.mcpServers = [{ ...HOSTED_ROW, visibility: "disabled" }];
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
    // The whole record is carried over; only visibility changes.
    expect(mocks.updateMcpServer).toHaveBeenCalledWith({
      request: {
        updateMcpServerForm: expect.objectContaining({
          id: "server-hosted",
          visibility: "private",
          name: "Hosted",
          toolsetId: "toolset-1",
          environmentId: "env-1",
          toolVariationsGroupId: "tvg-1",
        }),
      },
    });
    expect(mocks.toastSuccess).toHaveBeenCalledWith("MCP server enabled");
  });

  // A hosted server is granted under its toolset id (the server resolves the
  // grant the same way), so a grant naming the toolset attaches the verbs and
  // one naming an unrelated server does not.
  it("scopes a hosted server's verbs to the grant covering its toolset", () => {
    mocks.scopes = ["mcp:write"];
    mocks.toolsets = [{ id: "toolset-1", name: "Hosted", slug: "hosted" }];
    mocks.mcpServers = [{ ...HOSTED_ROW, visibility: "private" }];

    mocks.mcpWriteResourceIds = ["toolset-1"];
    const covered = renderCandidates();
    const [hostedCovered] = byKind(covered.result.current, "mcp_server");
    expect(hostedCovered?.detail).toBe("MCP server · enabled");
    expect(hostedCovered?.verbs).toEqual(["open", "disable"]);

    mocks.mcpWriteResourceIds = ["server-other"];
    const uncovered = renderCandidates();
    const [hostedUncovered] = byKind(uncovered.result.current, "mcp_server");
    expect(hostedUncovered?.verbs).toEqual(["open"]);
  });

  // The wrapper row's own id is not what the grant names for a hosted server.
  it("does not attach verbs to a hosted server for a grant naming only its wrapper row", () => {
    mocks.scopes = ["mcp:write"];
    mocks.mcpWriteResourceIds = ["server-hosted"];
    mocks.toolsets = [{ id: "toolset-1", name: "Hosted", slug: "hosted" }];
    mocks.mcpServers = [{ ...HOSTED_ROW, visibility: "private" }];
    const { result } = renderCandidates();
    const [hosted] = byKind(result.current, "mcp_server");

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
    // The update replaces the whole record, so the backend ids and the
    // environment ride along unchanged.
    expect(mocks.updateMcpServer).toHaveBeenCalledWith({
      request: {
        updateMcpServerForm: expect.objectContaining({
          id: "server-slack-a1",
          visibility: "disabled",
          name: "Slack",
          remoteMcpServerId: "remote-1",
          environmentId: "env-1",
        }),
      },
    });
    expect(mocks.invalidateMcpServerQueries).toHaveBeenCalled();
    expect(mocks.toastSuccess).toHaveBeenCalledWith("MCP server disabled");
  });

  // The MCP page's gate: without a reader or writer scope the listing is not
  // reachable, so the palette neither shows nor fetches it.
  it("stays hidden, and does not fetch, without mcp:read or mcp:write", () => {
    mocks.scopes = ["project:read"];
    mocks.mcpServers = [mcpServer("Slack", "slack-a1", "private")];
    mocks.toolsets = [{ id: "toolset-1", name: "Hosted", slug: "hosted" }];
    const { result } = renderCandidates();

    expect(byKind(result.current, "mcp_server")).toHaveLength(0);
    for (const name of ["useListToolsets", "useMcpServers"]) {
      for (const args of mocks.calls[name] ?? []) {
        expect(optionsOf(args).enabled, name).toBe(false);
      }
    }
  });

  // The update replaces the whole record, so it must be built from the row as
  // it is now: a name or environment changed since the list was cached would
  // otherwise be overwritten with the stale values.
  it("builds the update from a freshly fetched row, not the cached one", async () => {
    mocks.scopes = ["mcp:write"];
    mocks.mcpServers = [mcpServer("Slack", "slack-a1", "private")];
    mocks.latestServers["server-slack-a1"] = {
      ...mcpServer("Slack (renamed)", "slack-a1", "private"),
      environmentId: "env-2",
    };
    mocks.updateMcpServer.mockResolvedValue({});
    const { result } = renderCandidates();
    const [slack] = byKind(result.current, "mcp_server");

    await act(async () => {
      await slack?.run("disable");
    });
    expect(mocks.fetchQuery).toHaveBeenCalledWith(
      expect.objectContaining({
        queryKey: ["getMcpServer", "server-slack-a1"],
        staleTime: 0,
      }),
    );
    expect(mocks.updateMcpServer).toHaveBeenCalledWith({
      request: {
        updateMcpServerForm: expect.objectContaining({
          id: "server-slack-a1",
          visibility: "disabled",
          name: "Slack (renamed)",
          environmentId: "env-2",
        }),
      },
    });
  });

  it("skips the update when the fresh row already has the requested visibility", async () => {
    mocks.scopes = ["mcp:write"];
    mocks.mcpServers = [mcpServer("Slack", "slack-a1", "private")];
    mocks.latestServers["server-slack-a1"] = mcpServer(
      "Slack",
      "slack-a1",
      "disabled",
    );
    const { result } = renderCandidates();
    const [slack] = byKind(result.current, "mcp_server");

    await act(async () => {
      await slack?.run("disable");
    });
    expect(mocks.updateMcpServer).not.toHaveBeenCalled();
    expect(mocks.invalidateMcpServerQueries).toHaveBeenCalled();
    expect(mocks.toastSuccess).toHaveBeenCalledWith("MCP server disabled");
  });

  it("toasts and rethrows when the pre-update fetch fails", async () => {
    mocks.scopes = ["mcp:write"];
    mocks.mcpServers = [mcpServer("Jira", "jira-b2", "disabled")];
    mocks.fetchQuery.mockRejectedValue(new Error("offline"));
    const { result } = renderCandidates();
    const [jira] = byKind(result.current, "mcp_server");

    await expect(jira?.run("enable")).rejects.toThrow("offline");
    expect(mocks.updateMcpServer).not.toHaveBeenCalled();
    expect(mocks.toastError).toHaveBeenCalledWith("offline");
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
    mocks.scopes = ["project:read", "mcp:read"];
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

  // `upToDate` is tri-state: a connected legacy project can report no
  // freshness at all, and that must not read as current.
  it("reads unknown publish freshness as connected, not up to date", () => {
    mocks.publishStatus = { configured: true, connected: true };
    const { result } = renderCandidates();
    const [marketplace] = byKind(result.current, "marketplace");

    expect(marketplace?.detail).toBe("Plugin marketplace · connected");
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
  // The stored entries are keyed per user, and until the session resolves
  // whatever the hook last read belongs to somebody else's key.
  it("yields nothing, and does not read, until the user id has resolved", () => {
    mocks.recents = [{ label: "Slack", href: "/acme/mcp/slack", visitedAt: 1 }];
    const { result } = renderCandidates({ recentsUserId: null });

    expect(byKind(result.current, "recent")).toHaveLength(0);
    expect(mocks.recentsArgs.length).toBeGreaterThan(0);
    for (const args of mocks.recentsArgs) {
      expect(args).toEqual([undefined, "acme", "default", false]);
    }
  });

  it("reads under the resolved user id only while the palette is open", () => {
    renderCandidates({ enabled: false });
    for (const args of mocks.recentsArgs) {
      expect(args).toEqual(["user_1", "acme", "default", false]);
    }

    mocks.recentsArgs = [];
    renderCandidates();
    expect(mocks.recentsArgs.at(-1)).toEqual([
      "user_1",
      "acme",
      "default",
      true,
    ]);
  });

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

describe("useSourceCandidates", () => {
  beforeEach(() => {
    // Sources are a tab of the MCP section, behind its reader gate.
    mocks.scopes = ["mcp:read"];
    mocks.deployment = {
      openapiv3Assets: [
        { id: "doc-1", name: "Petstore", slug: "petstore", assetId: "a-1" },
      ],
      functionsAssets: [
        { id: "fn-1", name: "Billing", slug: "billing", assetId: "a-2" },
      ],
      externalMcps: [
        {
          id: "ext-1",
          name: "Linear",
          slug: "linear",
          registryServerSpecifier: "x",
        },
      ],
    };
  });

  // The Sources page addresses a document or function by its deployment asset
  // id; the palette must open the same page rather than the MCP index.
  it("opens an OpenAPI document or function on its own source page", () => {
    const { result } = renderCandidates();
    const [petstore, billing] = byKind(result.current, "source");

    expect(petstore?.detail).toBe("Source · OpenAPI");
    expect(petstore?.keywords).toEqual(
      expect.arrayContaining(["petstore", "openapi", "doc-1"]),
    );
    void petstore?.run("open");
    expect(mocks.goToSourceDetail).toHaveBeenLastCalledWith("doc-1");

    expect(billing?.detail).toBe("Source · function");
    void billing?.run("open");
    expect(mocks.goToSourceDetail).toHaveBeenLastCalledWith("fn-1");
  });

  // An external MCP has no page of its own: it opens the hosted server built
  // from it, found through the toolset whose tool URNs carry the source slug.
  it("opens an external MCP on the hosted server that carries its tools", () => {
    mocks.scopes = ["mcp:read"];
    mocks.toolsets = [
      {
        id: "toolset-1",
        name: "Linear",
        slug: "linear-srv",
        toolUrns: ["tools:externalmcp:linear:create_issue"],
      },
    ];
    const { result } = renderCandidates();
    const [linear] = byKind(result.current, "source").filter(
      (c) => c.detail === "Source · external MCP",
    );

    expect(linear?.keywords).toEqual(
      expect.arrayContaining(["linear", "externalmcp", "ext-1"]),
    );
    void linear?.run("open");
    expect(mocks.goToToolsetDetails).toHaveBeenCalledWith("linear-srv");
    expect(mocks.goToSources).not.toHaveBeenCalled();
  });

  it("falls back to the sources list for an external MCP no server uses", () => {
    mocks.scopes = ["mcp:read"];
    mocks.toolsets = [
      { id: "toolset-2", name: "Other", slug: "other", toolUrns: [] },
    ];
    const { result } = renderCandidates();
    const [linear] = byKind(result.current, "source").filter(
      (c) => c.detail === "Source · external MCP",
    );

    void linear?.run("open");
    expect(mocks.goToSources).toHaveBeenCalled();
    expect(mocks.goToToolsetDetails).not.toHaveBeenCalled();
  });

  // Several servers can carry the same external MCP; opening the first would
  // land on an arbitrary server, so the list is the honest target (as the
  // legacy source redirect resolves it).
  it("falls back to the sources list when several servers carry the same external MCP", () => {
    mocks.scopes = ["mcp:read"];
    mocks.toolsets = [
      {
        id: "toolset-1",
        name: "Linear",
        slug: "linear-srv",
        toolUrns: ["tools:externalmcp:linear:create_issue"],
      },
      {
        id: "toolset-2",
        name: "Linear for support",
        slug: "linear-support",
        toolUrns: ["tools:externalmcp:linear:list_issues"],
      },
    ];
    const { result } = renderCandidates();
    const [linear] = byKind(result.current, "source").filter(
      (c) => c.detail === "Source · external MCP",
    );

    void linear?.run("open");
    expect(mocks.goToSources).toHaveBeenCalled();
    expect(mocks.goToToolsetDetails).not.toHaveBeenCalled();
  });
});

describe("usePolicyCandidates", () => {
  const policies = [
    {
      id: "p-1",
      name: "Block secrets",
      action: "block",
      policyType: "standard",
    },
    {
      id: "p-2",
      name: "No customer data",
      action: "flag",
      policyType: "prompt_based",
    },
  ];

  // The Guardrails page lists prompt-based policies only behind
  // gram-prompt-policies; the palette must not offer what the page hides.
  it("hides prompt-based policies while the feature flag is off", () => {
    mocks.scopes = ["org:admin"];
    mocks.policies = policies;
    const { result } = renderCandidates();

    expect(byKind(result.current, "policy").map((c) => c.title)).toEqual([
      "Block secrets",
    ]);
  });

  it("lists prompt-based policies once the feature flag is on", () => {
    mocks.scopes = ["org:admin"];
    mocks.flags = ["gram-prompt-policies"];
    mocks.policies = policies;
    const { result } = renderCandidates();

    expect(byKind(result.current, "policy").map((c) => c.title)).toEqual([
      "Block secrets",
      "No customer data",
    ]);
  });
});

describe("useRuleCandidates", () => {
  // The rules page resolves `?rule=` by the stable `custom.*` rule id; the
  // row's database uuid opens nothing there.
  it("addresses a custom rule by its custom.* rule id, not its database id", () => {
    mocks.scopes = ["org:admin"];
    mocks.rules = [
      {
        id: "0f0e0d0c-0000-4000-8000-000000000001",
        ruleId: "custom.pii-ssn",
        title: "SSN in prompt",
        severity: "high",
      },
    ];
    const { result } = renderCandidates();
    const [custom] = byKind(result.current, "rule").filter(
      (c) => c.title === "SSN in prompt",
    );

    expect(custom?.id).toBe("rule:custom.pii-ssn");
    expect(custom?.keywords).toContain("custom.pii-ssn");
    expect(custom?.keywords).not.toContain(
      "0f0e0d0c-0000-4000-8000-000000000001",
    );
    void custom?.run("open");
    expect(mocks.navigate).toHaveBeenCalledWith(
      "/acme/projects/default/policy-center?tab=detection-rules&rule=custom.pii-ssn",
    );
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

  // The dashboard's query defaults throw non-401/403 errors to the nearest
  // error boundary, and the palette has none: every source must opt out so a
  // failing endpoint degrades to no candidates instead of a blank palette.
  it("never lets an SDK query throw to an error boundary", () => {
    mocks.scopes = ["org:read", "org:admin", "mcp:read", "project:read"];
    renderCandidates();

    const names = Object.keys(mocks.calls);
    expect(names.length).toBeGreaterThan(0);
    for (const name of names) {
      for (const args of mocks.calls[name] ?? []) {
        expect(optionsOf(args).throwOnError, name).toBe(false);
      }
    }
  });

  // The SDK folds gramProject into the query key; without it one cache entry
  // would serve every project and a switch would show the last one's rows.
  it("keys every project-scoped query by the current project", () => {
    mocks.scopes = ["org:read", "org:admin", "mcp:read", "project:read"];
    renderCandidates();

    for (const name of [
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
    ]) {
      expect(mocks.calls[name], name).toBeDefined();
      for (const args of mocks.calls[name] ?? []) {
        expect(requestOf(args)?.gramProject, name).toBe("default");
      }
    }
  });

  // The Plugins page is reached through project:read or project:write; a
  // member with neither never fires its list call from the palette.
  it("does not fetch plugins without a scope that reaches the Plugins page", () => {
    mocks.scopes = ["mcp:read"];
    renderCandidates();
    for (const args of mocks.calls["usePlugins"] ?? []) {
      expect(optionsOf(args).enabled).toBe(false);
    }

    mocks.calls = {};
    mocks.scopes = ["project:write"];
    renderCandidates();
    expect(mocks.calls["usePlugins"]?.length).toBeGreaterThan(0);
    for (const args of mocks.calls["usePlugins"] ?? []) {
      expect(optionsOf(args).enabled).toBe(true);
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
