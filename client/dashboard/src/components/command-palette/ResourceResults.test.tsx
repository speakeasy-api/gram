import { Command, CommandInput, CommandList } from "@/components/ui/Command";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  toolsets: [] as unknown[],
  mcpServers: [] as unknown[],
  goToToolsetDetails: vi.fn(),
  goToMcpServerOverview: vi.fn(),
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
  useRiskListCustomDetectionRulesSuspense: () => ({ data: { rules: [] } }),
}));
vi.mock("@gram/client/react-query/listMcpApprovalRequests.js", () => ({
  useListMcpApprovalRequestsSuspense: () => ({ data: { requests: [] } }),
}));
vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  useRiskListPoliciesSuspense: () => ({ data: { policies: [] } }),
}));
vi.mock("@gram/client/react-query/plugins", () => ({
  usePluginsSuspense: () => ({ data: { plugins: [] } }),
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

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "default" }),
  useProjectSlugForRequests: () => "default",
}));

// Non-admin: keeps the risk/approval groups out of the tree entirely.
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => false, hasScope: () => false }),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      details: { goTo: mocks.goToToolsetDetails },
      x: { overview: { goTo: mocks.goToMcpServerOverview } },
    },
  }),
}));

vi.mock("@/components/ui/Icon", () => ({
  Icon: ({ name }: { name: string }) => <span data-icon={name} />,
}));

import { ResourceResults } from "./ResourceResults";

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

describe("ResourceResults MCP Servers group", () => {
  beforeEach(() => {
    mocks.toolsets = [];
    mocks.mcpServers = [];
    mocks.goToToolsetDetails.mockClear();
    mocks.goToMcpServerOverview.mockClear();
  });
  afterEach(cleanup);

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
