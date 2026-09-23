import { MemoryRouter } from "react-router";
import { Command, CommandInput, CommandList } from "@/components/ui/Command";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  pluginList: vi.fn(() => ({ data: { plugins: [] } })),
  policies: vi.fn(() => ({ data: { policies: [] } })),
  approvals: vi.fn(() => ({ data: { requests: [] } })),
  rules: vi.fn(() => ({ data: { rules: [] } })),
  members: vi.fn(() => ({ data: { members: [] } })),
  catalog: vi.fn(() => ({ data: { servers: mocks.catalogServers } })),
  toolsets: [] as unknown[],
  mcpServers: [] as unknown[],
  catalogServers: [] as unknown[],
  /** Scopes the RBAC mock reports; empty keeps the gated groups unmounted. */
  scopes: [] as string[],
  blocked: [] as { scope: string; selectors: { resourceId: string }[] }[],
  orgSlug: "acme" as string | undefined,
  projectSlug: "default" as string | undefined,
  organizations: [] as unknown[],
  activeOrganizationId: "",
  goToToolsetDetails: vi.fn(),
  goToMcpServerOverview: vi.fn(),
  goToCatalogDetail: vi.fn(),
  navigate: vi.fn(),
  clearQueryCache: vi.fn(),
}));

// The palette's other groups are irrelevant here; stub them empty so this file
// only exercises the MCP Servers group.
vi.mock("@gram/client/react-query/assistantsList.js", () => ({
  useAssistantsListSuspense: () => ({ data: { assistants: [] } }),
}));
vi.mock("@gram/client/react-query/latestDeployment.js", () => ({
  useLatestDeploymentSuspense: () => ({ data: { deployment: undefined } }),
}));
vi.mock("@gram/client/react-query/listDeployments.js", () => ({
  useListDeploymentsSuspense: () => ({ data: { items: [] } }),
}));
vi.mock("@gram/client/react-query/riskListCustomDetectionRules.js", () => ({
  useRiskListCustomDetectionRulesSuspense: mocks.rules,
}));
vi.mock("@gram/client/react-query/listMcpApprovalRequests.js", () => ({
  useListMcpApprovalRequestsSuspense: mocks.approvals,
}));
vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  useRiskListPoliciesSuspense: mocks.policies,
}));
vi.mock("@gram/client/react-query/plugins", () => ({
  usePluginsSuspense: mocks.pluginList,
}));
vi.mock("@/pages/environments/useEnvironments", () => ({
  useEnvironments: () => [],
}));

vi.mock("@gram/client/react-query/listToolsets.js", () => ({
  useListToolsetsSuspense: () => ({ data: { toolsets: mocks.toolsets } }),
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServersSuspense: () => ({ data: { mcpServers: mocks.mcpServers } }),
}));
vi.mock("@gram/client/react-query/listMCPCatalog.js", () => ({
  useListMCPCatalogSuspense: mocks.catalog,
}));

vi.mock("@gram/client/react-query/members.js", () => ({
  useMembersSuspense: mocks.members,
}));

vi.mock("@gram/client/react-query/sessionInfo.js", () => ({
  useSessionInfoSuspense: () => ({
    data: {
      result: {
        organizations: mocks.organizations,
        activeOrganizationId: mocks.activeOrganizationId,
      },
    },
  }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({
    orgSlug: mocks.orgSlug,
    projectSlug: mocks.projectSlug,
  }),
  useProjectSlugForRequests: () => mocks.projectSlug ?? "default",
}));

// The projects group navigates by path (there is no per-project route entry)
// and drops the query cache on a switch, so both are stubbed here.
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useNavigate: () => mocks.navigate,
  useLocation: () => ({ search: "" }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ clear: mocks.clearQueryCache }),
}));

// Exercise the real RBAC hook, including resource-specific exclusion grants.
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-a" }),
  useProject: () => ({ id: "project-a" }),
  useSession: () => ({ session: "session-a" }),
  useIsPlatformAdmin: () => false,
}));
vi.mock("@gram/client/react-query/grants.js", () => ({
  useGrants: () => ({
    data: {
      grants: [...mocks.scopes.map((scope) => ({ scope })), ...mocks.blocked],
    },
    isLoading: false,
  }),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      details: { goTo: mocks.goToToolsetDetails },
      x: { overview: { goTo: mocks.goToMcpServerOverview } },
      catalog: { detail: { goTo: mocks.goToCatalogDetail } },
    },
  }),
}));

vi.mock("@/components/ui/Icon", () => ({
  Icon: ({ name }: { name: string }) => <span data-icon={name} />,
}));

vi.mock("@/pages/plugins/usePluginQueryScope", () => ({
  usePluginQueryScope: () => ({
    gramProject: "project-a",
    gramSession: "session-a",
  }),
}));
import {
  PeopleResults,
  ProjectsResults,
  ResourceResults,
} from "./ResourceResults";

function toolset(name: string, slug: string) {
  return { id: `toolset-${slug}`, name, slug };
}

function mcpServer(
  name: string | undefined,
  slug: string | undefined,
  backing: Record<string, string>,
) {
  return { id: `server-${slug ?? "no-slug"}`, name, slug, ...backing };
}

const REMOTE = { remoteMcpServerId: "remote-1" };
const TUNNELED = { tunneledMcpServerId: "tunneled-1" };
const UNPROXIED = { unproxiedMcpServerId: "unproxied-1" };

// Renders the palette's resource results inside a real cmdk root, so the
// assertions run against cmdk's own filtering rather than a reimplementation
// of it.
function renderResults(query = "") {
  const result = render(
    <Command shouldFilter>
      <CommandInput />
      <CommandList>
        <ResourceResults onNavigate={() => {}} query={query} />
      </CommandList>
    </Command>,
  );
  if (query) {
    fireEvent.change(result.container.querySelector("input")!, {
      target: { value: query },
    });
  }
  return result;
}

function catalogServer(title: string, registrySpecifier: string) {
  return { title, registrySpecifier, registryId: "registry-1" };
}

function renderProjects(query = "") {
  const result = render(
    <Command shouldFilter>
      <CommandInput />
      <CommandList>
        <ProjectsResults onNavigate={() => {}} />
      </CommandList>
    </Command>,
  );
  if (query) {
    fireEvent.change(result.container.querySelector("input")!, {
      target: { value: query },
    });
  }
  return result;
}

function project(name: string, slug: string) {
  return { id: `project-${slug}`, name, slug };
}

function resetMocks() {
  vi.clearAllMocks();
  mocks.toolsets = [];
  mocks.mcpServers = [];
  mocks.catalogServers = [];
  mocks.scopes = [];
  mocks.blocked = [];
  mocks.orgSlug = "acme";
  mocks.projectSlug = "default";
  mocks.organizations = [];
  mocks.activeOrganizationId = "";
  mocks.goToToolsetDetails.mockClear();
  mocks.goToMcpServerOverview.mockClear();
  mocks.goToCatalogDetail.mockClear();
  mocks.navigate.mockClear();
  mocks.clearQueryCache.mockClear();
}

describe("ResourceResults MCP Servers group", () => {
  beforeEach(resetMocks);
  afterEach(cleanup);

  it("does not query plugins for baseline project readers", () => {
    mocks.scopes = ["project:read"];
    renderResults();
    expect(mocks.pluginList).not.toHaveBeenCalled();
  });
  it("does not mount plugins when organization read is explicitly blocked", () => {
    mocks.scopes = ["org:read"];
    mocks.blocked = [
      { scope: "org:blocked_read", selectors: [{ resourceId: "org-a" }] },
    ];
    renderResults();
    expect(mocks.pluginList).not.toHaveBeenCalled();
  });
  it.each(["org:blocked_read", "org:blocked_admin"])(
    "does not mount admin queries when %s targets the active organization",
    (scope) => {
      mocks.scopes = ["org:admin"];
      mocks.blocked = [{ scope, selectors: [{ resourceId: "org-a" }] }];
      renderResults("search");
      expect(mocks.policies).not.toHaveBeenCalled();
      expect(mocks.rules).not.toHaveBeenCalled();
      expect(mocks.approvals).not.toHaveBeenCalled();
    },
  );
  it("does not mount people when organization read is blocked", () => {
    mocks.scopes = ["org:read"];
    mocks.blocked = [
      { scope: "org:blocked_read", selectors: [{ resourceId: "org-a" }] },
    ];
    render(
      <MemoryRouter>
        <Command>
          <CommandList>
            <PeopleResults onNavigate={() => {}} />
          </CommandList>
        </Command>
      </MemoryRouter>,
    );
    expect(mocks.members).not.toHaveBeenCalled();
  });
  it("does not mount catalog when project read is blocked", () => {
    mocks.scopes = ["project:read"];
    mocks.blocked = [
      {
        scope: "project:blocked_read",
        selectors: [{ resourceId: "project-a" }],
      },
    ];
    renderResults("search");
    expect(mocks.catalog).not.toHaveBeenCalled();
  });
  it("ignores another organization's read block", () => {
    mocks.scopes = ["org:read"];
    mocks.blocked = [
      { scope: "org:blocked_read", selectors: [{ resourceId: "org-b" }] },
    ];
    renderResults();
    expect(mocks.pluginList).toHaveBeenCalledWith({
      gramProject: "project-a",
      gramSession: "session-a",
    });
  });
  it("queries plugins for project plugin writers without org read", () => {
    mocks.scopes = ["plugin:write"];
    renderResults();
    expect(mocks.pluginList).toHaveBeenCalledWith({
      gramProject: "project-a",
      gramSession: "session-a",
    });
  });
  it("lists toolset-backed and mcp_servers-backed servers under one heading", () => {
    mocks.toolsets = [toolset("Hosted Server", "hosted-server")];
    mocks.mcpServers = [
      mcpServer("Remote Server", "remote-server", REMOTE),
      mcpServer("Tunneled Server", "tunneled-server", TUNNELED),
      mcpServer("Unproxied Server", "unproxied-server", UNPROXIED),
    ];
    renderResults();

    expect(screen.getAllByText("MCP Servers")).not.toHaveLength(0);
    expect(screen.getByText("Hosted Server")).toBeTruthy();
    expect(screen.getByText("Remote Server")).toBeTruthy();
    expect(screen.getByText("Tunneled Server")).toBeTruthy();
    expect(screen.getByText("Unproxied Server")).toBeTruthy();
  });

  // The regression this guards: a toolset-backed mcp_servers row is the same
  // server the toolsets fetch already returned, so surfacing both would show
  // every hosted server twice.
  it("omits toolset-backed mcp_servers rows so hosted servers aren't doubled", () => {
    mocks.toolsets = [toolset("Hosted Server", "hosted-server")];
    mocks.mcpServers = [
      mcpServer("Hosted Server", "hosted-server-dupe", {
        toolsetId: "toolset-hosted-server",
      }),
    ];
    renderResults();

    expect(screen.getAllByText("Hosted Server")).toHaveLength(1);
    expect(screen.queryByText("hosted-server-dupe")).toBeNull();
  });

  it("renders nothing when both collections are empty", () => {
    renderResults();
    expect(screen.queryByText("MCP Servers")).toBeNull();
  });

  it("finds an mcp_servers-backed server by name", () => {
    mocks.mcpServers = [
      mcpServer("Linear", "linear-a1b2c3", REMOTE),
      mcpServer("Notion", "notion-d4e5f6", REMOTE),
    ];
    renderResults("linear");

    expect(screen.getByText("Linear")).toBeTruthy();
    expect(screen.queryByText("Notion")).toBeNull();
  });

  // Servers get a generated slug suffix, so the slug is often the only thing a
  // user can recall exactly — it has to be searchable, matching toolset rows.
  it("finds an mcp_servers-backed server by slug", () => {
    mocks.mcpServers = [
      mcpServer("Linear", "linear-a1b2c3", REMOTE),
      mcpServer("Notion", "notion-d4e5f6", REMOTE),
    ];
    renderResults("d4e5f6");

    expect(screen.getByText("Notion")).toBeTruthy();
    expect(screen.queryByText("Linear")).toBeNull();
  });

  it("navigates to the mcp_servers-backed route on select", () => {
    mocks.mcpServers = [mcpServer("Remote Server", "remote-server", REMOTE)];
    renderResults();

    fireEvent.click(screen.getByText("Remote Server"));

    expect(mocks.goToMcpServerOverview).toHaveBeenCalledWith("remote-server");
    expect(mocks.goToToolsetDetails).not.toHaveBeenCalled();
  });

  it("navigates to the toolset route for toolset-backed servers", () => {
    mocks.toolsets = [toolset("Hosted Server", "hosted-server")];
    renderResults();

    fireEvent.click(screen.getByText("Hosted Server"));

    expect(mocks.goToToolsetDetails).toHaveBeenCalledWith("hosted-server");
    expect(mocks.goToMcpServerOverview).not.toHaveBeenCalled();
  });

  // name and slug are both optional on the McpServer wire type.
  it("falls back to a label and the id route param when name/slug are absent", () => {
    mocks.mcpServers = [mcpServer(undefined, undefined, REMOTE)];
    renderResults();

    fireEvent.click(screen.getByText("MCP Server"));

    expect(mocks.goToMcpServerOverview).toHaveBeenCalledWith("server-no-slug");
  });
});

describe("ResourceResults MCP Catalog group", () => {
  beforeEach(() => {
    resetMocks();
    mocks.scopes = ["project:read"];
  });
  afterEach(cleanup);

  // The registry runs to hundreds of entries and costs an upstream round trip,
  // so it stays out of the idle palette the way detection rules do.
  it("stays out of the palette until the user types", () => {
    mocks.catalogServers = [catalogServer("Datadog", "com.datadoghq/datadog")];
    renderResults();

    expect(screen.queryByText("MCP Catalog")).toBeNull();
    expect(screen.queryByText("Datadog")).toBeNull();
  });

  it("lists matching catalog entries once the user types", () => {
    mocks.catalogServers = [
      catalogServer("Datadog", "com.datadoghq/datadog"),
      catalogServer("Notion", "com.notion/notion"),
    ];
    renderResults("datadog");

    expect(screen.getAllByText("MCP Catalog")).not.toHaveLength(0);
    expect(screen.getByText("Datadog")).toBeTruthy();
    expect(screen.queryByText("Notion")).toBeNull();
  });

  // The registry coordinate is often what someone remembers, and it is what
  // the entry is addressed by.
  it("finds a catalog entry by its registry specifier", () => {
    mocks.catalogServers = [
      catalogServer("Datadog", "com.datadoghq/datadog"),
      catalogServer("Notion", "com.notion/notion"),
    ];
    renderResults("com.notion");

    expect(screen.getByText("Notion")).toBeTruthy();
    expect(screen.queryByText("Datadog")).toBeNull();
  });

  // The distinction the group exists to make: a name a project already runs
  // and offers in the catalog produces one row under each heading, so it is
  // never ambiguous which one opens the running server.
  it("keeps catalog entries apart from servers the project already runs", () => {
    mocks.mcpServers = [mcpServer("Datadog", "datadog-a1b2c3", REMOTE)];
    mocks.catalogServers = [catalogServer("Datadog", "com.datadoghq/datadog")];
    renderResults("datadog");

    expect(screen.getAllByText("MCP Servers")).not.toHaveLength(0);
    expect(screen.getAllByText("MCP Catalog")).not.toHaveLength(0);
    expect(screen.getByText("datadog-a1b2c3")).toBeTruthy();
    expect(screen.getByText("com.datadoghq/datadog")).toBeTruthy();
  });

  it("opens the catalog entry's detail page on select", () => {
    mocks.catalogServers = [catalogServer("Datadog", "com.datadoghq/datadog")];
    renderResults("datadog");

    fireEvent.click(screen.getByText("Datadog"));

    expect(mocks.goToCatalogDetail).toHaveBeenCalledWith(
      encodeURIComponent("com.datadoghq/datadog"),
    );
    expect(mocks.goToMcpServerOverview).not.toHaveBeenCalled();
  });

  // Standing in as the label, the specifier is not repeated as the sublabel.
  it("falls back to the specifier when the registry publishes no title", () => {
    mocks.catalogServers = [
      { registrySpecifier: "io.github.acme/widgets", registryId: "registry-1" },
    ];
    renderResults("widgets");

    expect(screen.getAllByText("io.github.acme/widgets")).toHaveLength(1);
  });

  // The detail route is addressed by specifier alone and resolves the first
  // match, so a second registry publishing the same server would otherwise add
  // an identical-looking row that leads to the very same page.
  it("offers one row per specifier when two registries publish the same server", () => {
    mocks.catalogServers = [
      { ...catalogServer("Datadog", "com.datadoghq/datadog") },
      {
        ...catalogServer("Datadog", "com.datadoghq/datadog"),
        registryId: "registry-2",
      },
    ];
    renderResults("datadog");

    expect(screen.getAllByText("Datadog")).toHaveLength(1);
  });

  it("stays hidden for a user without catalog access", () => {
    mocks.scopes = [];
    mocks.catalogServers = [catalogServer("Datadog", "com.datadoghq/datadog")];
    renderResults("datadog");

    expect(screen.queryByText("MCP Catalog")).toBeNull();
  });

  // listCatalog requires project:read. Mounting the group for a writer who
  // lacks it would fire a request the backend refuses.
  it("stays hidden for a writer without project:read", () => {
    mocks.scopes = ["mcp:write"];
    mocks.catalogServers = [catalogServer("Datadog", "com.datadoghq/datadog")];
    renderResults("datadog");

    expect(screen.queryByText("MCP Catalog")).toBeNull();
  });
});

describe("ProjectsResults", () => {
  beforeEach(resetMocks);
  afterEach(cleanup);

  it("lists the organization's projects in slug order", () => {
    mocks.organizations = [
      {
        id: "org-acme",
        slug: "acme",
        projects: [
          project("Widgets", "widgets"),
          project("Billing", "billing"),
        ],
      },
    ];
    renderProjects();

    expect(screen.getAllByText("Projects")).not.toHaveLength(0);
    const labels = screen
      .getAllByRole("option")
      .map((option) => option.textContent);
    expect(labels).toEqual(["Billing", "Widgets"]);
  });

  it("renders nothing when the organization has no projects", () => {
    mocks.organizations = [{ id: "org-acme", slug: "acme", projects: [] }];
    renderProjects();

    expect(screen.queryByText("Projects")).toBeNull();
  });

  it("finds a project by name", () => {
    mocks.organizations = [
      {
        id: "org-acme",
        slug: "acme",
        projects: [
          project("Widgets", "widgets-a1b2"),
          project("Billing", "billing"),
        ],
      },
    ];
    renderProjects("widgets");

    expect(screen.getByText("Widgets")).toBeTruthy();
    expect(screen.queryByText("Billing")).toBeNull();
  });

  // A renamed project keeps its original slug, so the slug is often the only
  // thing that still matches what the URL showed.
  it("finds a project by slug", () => {
    mocks.organizations = [
      {
        id: "org-acme",
        slug: "acme",
        projects: [
          project("Widgets", "widgets"),
          project("Payments", "billing"),
        ],
      },
    ];
    renderProjects("billing");

    expect(screen.getByText("Payments")).toBeTruthy();
    expect(screen.queryByText("Widgets")).toBeNull();
  });

  it("finds a project by id", () => {
    mocks.organizations = [
      {
        id: "org-acme",
        slug: "acme",
        projects: [
          project("Widgets", "widgets"),
          project("Billing", "billing"),
        ],
      },
    ];
    renderProjects("project-billing");

    expect(screen.getByText("Billing")).toBeTruthy();
    expect(screen.queryByText("Widgets")).toBeNull();
  });

  it("opens the project and drops the cache when switching projects", () => {
    mocks.projectSlug = "widgets";
    mocks.organizations = [
      {
        id: "org-acme",
        slug: "acme",
        projects: [project("Billing", "billing")],
      },
    ];
    renderProjects();

    fireEvent.click(screen.getByText("Billing"));

    expect(mocks.navigate).toHaveBeenCalledWith("/acme/projects/billing");
    expect(mocks.clearQueryCache).toHaveBeenCalled();
  });

  // Selecting the project you are already in is a jump to its overview, not a
  // switch — dropping the cache there would refetch the whole page for nothing.
  it("keeps the cache when opening the project already in the URL", () => {
    mocks.projectSlug = "billing";
    mocks.organizations = [
      {
        id: "org-acme",
        slug: "acme",
        projects: [project("Billing", "billing")],
      },
    ];
    renderProjects();

    fireEvent.click(screen.getByText("Billing"));

    expect(mocks.navigate).toHaveBeenCalledWith("/acme/projects/billing");
    expect(mocks.clearQueryCache).not.toHaveBeenCalled();
  });

  // The group's whole reason to exist: at the org level there is no project
  // slug in the path, so the organization has to come from the session.
  it("falls back to the active organization when the path carries no org slug", () => {
    mocks.orgSlug = undefined;
    mocks.projectSlug = undefined;
    mocks.activeOrganizationId = "org-other";
    mocks.organizations = [
      {
        id: "org-acme",
        slug: "acme",
        projects: [project("Widgets", "widgets")],
      },
      {
        id: "org-other",
        slug: "other",
        projects: [project("Billing", "billing")],
      },
    ];
    renderProjects();

    expect(screen.getByText("Billing")).toBeTruthy();
    expect(screen.queryByText("Widgets")).toBeNull();

    fireEvent.click(screen.getByText("Billing"));
    expect(mocks.navigate).toHaveBeenCalledWith("/other/projects/billing");
  });

  it("omits the slug when it only repeats the name", () => {
    mocks.organizations = [
      {
        id: "org-acme",
        slug: "acme",
        projects: [project("Default", "default")],
      },
    ];
    renderProjects();

    expect(screen.getAllByText(/default/i)).toHaveLength(1);
  });

  it("labels a nameless project with its slug", () => {
    mocks.organizations = [
      { id: "org-acme", slug: "acme", projects: [project("", "widgets")] },
    ];
    renderProjects();

    expect(screen.getAllByText("widgets")).toHaveLength(1);
  });
});
