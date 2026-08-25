import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const queryHooks = vi.hoisted(() => ({
  activity: vi.fn(),
  catalog: vi.fn(),
  policies: vi.fn(),
  plugins: vi.fn(),
  remoteServers: vi.fn(),
  results: vi.fn(),
  servers: vi.fn(),
}));

const requestProject = vi.hoisted(() => ({ slug: "request-project" }));

vi.mock("@/pages/catalog/hooks", () => ({
  useListMCPCatalog: queryHooks.catalog,
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => requestProject.slug,
}));
vi.mock("@gram/client/react-query/getMcpServerActivity.js", () => ({
  useGetMcpServerActivity: queryHooks.activity,
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: queryHooks.servers,
}));
vi.mock("@gram/client/react-query/plugins.js", () => ({
  usePlugins: queryHooks.plugins,
}));
vi.mock("@gram/client/react-query/remoteMcpServers.js", () => ({
  useRemoteMcpServers: queryHooks.remoteServers,
}));
vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  useRiskListPolicies: queryHooks.policies,
}));
vi.mock("@gram/client/react-query/riskListResults.js", () => ({
  useRiskListResults: queryHooks.results,
}));

import { useProjectGuideProgress } from "./useProjectGuideProgress";

beforeEach(() => {
  vi.clearAllMocks();
  requestProject.slug = "request-project";
  queryHooks.activity.mockReturnValue({
    data: { activity: [] },
    isPending: false,
  });
  queryHooks.catalog.mockReturnValue({
    data: {
      servers: [
        {
          meta: {},
          registrySpecifier: "example/catalog",
          remotes: [
            {
              transportType: "streamable-http",
              url: "https://catalog.example/mcp",
            },
          ],
        },
      ],
    },
    isError: false,
    isPending: false,
  });
  queryHooks.servers.mockReturnValue({
    data: { mcpServers: [] },
    isPending: false,
  });
  queryHooks.policies.mockReturnValue({
    data: { policies: [] },
    isPending: false,
  });
  queryHooks.plugins.mockReturnValue({
    data: { plugins: [] },
    isPending: false,
  });
  queryHooks.remoteServers.mockReturnValue({
    data: { remoteMcpServers: [] },
    isPending: false,
  });
  queryHooks.results.mockReturnValue({
    data: { results: [] },
    isPending: false,
  });
});

describe("useProjectGuideProgress", () => {
  it("keeps both journeys not started with known empty data", () => {
    const { result } = renderHook(() => useProjectGuideProgress());

    expect(result.current.statusByJourney).toEqual({
      "third-party-mcp": "not-started",
      "secret-block": "not-started",
    });
  });

  it("marks each journey in progress when its artifact exists", () => {
    queryHooks.servers.mockReturnValue({
      data: { mcpServers: [{ id: "server-1", remoteMcpServerId: "remote-1" }] },
      isPending: false,
    });
    queryHooks.remoteServers.mockReturnValue({
      data: {
        remoteMcpServers: [
          {
            id: "remote-1",
            url: "https://catalog.example/mcp",
          },
        ],
      },
      isPending: false,
    });
    queryHooks.policies.mockReturnValue({
      data: {
        policies: [
          {
            id: "policy-1",
            enabled: true,
            action: "block",
            audienceType: "everyone",
            policyType: "standard",
            sources: ["gitleaks"],
            messageTypes: ["tool_request", "tool_response"],
          },
        ],
      },
      isPending: false,
    });

    const { result } = renderHook(() => useProjectGuideProgress());

    expect(result.current.statusByJourney).toEqual({
      "third-party-mcp": "in-progress",
      "secret-block": "in-progress",
    });
  });

  it("does not persist progress from a near-miss secrets policy", () => {
    queryHooks.policies.mockReturnValue({
      data: {
        policies: [
          {
            id: "policy-1",
            enabled: true,
            action: "block",
            audienceType: "everyone",
            policyType: "standard",
            sources: ["gitleaks", "prompt_injection"],
            messageTypes: ["tool_request", "tool_response"],
          },
        ],
      },
      isPending: false,
    });

    const { result } = renderHook(() => useProjectGuideProgress());

    expect(result.current.statusByJourney["secret-block"]).toBe("not-started");
  });

  it("marks governed MCP activity and a secrets finding done", () => {
    queryHooks.servers.mockReturnValue({
      data: {
        mcpServers: [
          {
            id: "server-1",
            slug: "server-slug",
            remoteMcpServerId: "remote-1",
          },
        ],
      },
      isPending: false,
    });
    queryHooks.remoteServers.mockReturnValue({
      data: {
        remoteMcpServers: [
          {
            id: "remote-1",
            url: "https://catalog.example/mcp",
          },
        ],
      },
      isPending: false,
    });
    queryHooks.plugins.mockReturnValue({
      data: {
        plugins: [{ isDefault: true, servers: [{ mcpServerId: "server-1" }] }],
      },
      isPending: false,
    });
    queryHooks.activity.mockReturnValue({
      data: {
        activity: [
          {
            targetId: "server-slug",
            targetType: "hosted_mcp_server",
            totalToolCalls: 1,
          },
        ],
      },
      isPending: false,
    });
    queryHooks.policies.mockReturnValue({
      data: {
        policies: [
          {
            id: "policy-1",
            enabled: true,
            action: "block",
            audienceType: "everyone",
            policyType: "standard",
            sources: ["gitleaks"],
            messageTypes: ["tool_request", "tool_response"],
          },
        ],
      },
      isPending: false,
    });
    queryHooks.results.mockReturnValue({
      data: {
        results: [
          {
            id: "result-1",
            blockId: "block-1",
            createdAt: "2026-01-01T00:00:00Z",
            policyId: "policy-1",
            source: "gitleaks",
          },
        ],
      },
      isPending: false,
    });

    const { result } = renderHook(() => useProjectGuideProgress());

    expect(result.current.statusByJourney).toEqual({
      "third-party-mcp": "done",
      "secret-block": "done",
    });
  });

  it("does not complete the secret journey from an unrelated finding", () => {
    queryHooks.policies.mockReturnValue({
      data: {
        policies: [
          {
            id: "policy-1",
            enabled: true,
            action: "block",
            audienceType: "everyone",
            policyType: "standard",
            sources: ["gitleaks"],
            messageTypes: ["tool_request", "tool_response"],
          },
        ],
      },
      isPending: false,
    });
    queryHooks.results.mockReturnValue({
      data: {
        results: [
          {
            id: "result-1",
            blockId: "block-1",
            policyId: "other-policy",
            source: "gitleaks",
          },
        ],
      },
      isPending: false,
    });

    const { result } = renderHook(() => useProjectGuideProgress());

    expect(result.current.statusByJourney["secret-block"]).toBe("in-progress");
  });

  it("requests enough findings for latest selection", () => {
    renderHook(() => useProjectGuideProgress());

    expect(queryHooks.results).toHaveBeenCalledWith(
      {
        gramProject: "request-project",
        category: "secrets",
        limit: 200,
      },
      undefined,
      { throwOnError: false },
    );
  });

  it("credits activity for any governed catalog server", () => {
    queryHooks.servers.mockReturnValue({
      data: {
        mcpServers: [
          { id: "server-1", slug: "server-one", remoteMcpServerId: "remote-1" },
          { id: "server-2", slug: "server-two", remoteMcpServerId: "remote-2" },
        ],
      },
      isPending: false,
    });
    queryHooks.remoteServers.mockReturnValue({
      data: {
        remoteMcpServers: [
          { id: "remote-1", url: "https://custom.example/mcp" },
          { id: "remote-2", url: "https://catalog.example/mcp" },
        ],
      },
      isPending: false,
    });
    queryHooks.plugins.mockReturnValue({
      data: {
        plugins: [{ isDefault: true, servers: [{ mcpServerId: "server-2" }] }],
      },
      isPending: false,
    });
    queryHooks.activity.mockReturnValue({
      data: {
        activity: [
          {
            targetId: "server-two",
            targetType: "hosted_mcp_server",
            totalToolCalls: 1,
          },
        ],
      },
      isPending: false,
    });

    const { result } = renderHook(() => useProjectGuideProgress());

    expect(result.current.statusByJourney["third-party-mcp"]).toBe("done");
  });

  it("ignores custom, tunneled, and same-slug activity false positives", () => {
    queryHooks.servers.mockReturnValue({
      data: {
        mcpServers: [
          {
            id: "custom-server",
            remoteMcpServerId: "custom-remote",
            slug: "catalog-server",
          },
          {
            id: "tunneled-server",
            slug: "catalog-server",
            tunneledMcpServerId: "tunnel-1",
          },
        ],
      },
      isPending: false,
    });
    queryHooks.remoteServers.mockReturnValue({
      data: {
        remoteMcpServers: [
          { id: "custom-remote", url: "https://custom.example/mcp" },
        ],
      },
      isPending: false,
    });
    queryHooks.plugins.mockReturnValue({
      data: {
        plugins: [
          {
            isDefault: true,
            servers: [{ mcpServerId: "custom-server" }],
          },
        ],
      },
      isPending: false,
    });
    queryHooks.activity.mockReturnValue({
      data: {
        activity: [
          {
            targetId: "catalog-server",
            targetType: "tunneled_mcp_server",
            totalToolCalls: 2,
          },
        ],
      },
      isPending: false,
    });

    const { result } = renderHook(() => useProjectGuideProgress());

    expect(result.current.statusByJourney["third-party-mcp"]).toBe(
      "not-started",
    );
  });

  it("does not infer an empty or done status from unavailable query data", () => {
    queryHooks.servers.mockReturnValue({ data: undefined, isPending: false });
    queryHooks.results.mockReturnValue({ data: undefined, isPending: false });

    const { result } = renderHook(() => useProjectGuideProgress());

    expect(result.current.statusByJourney).toEqual({
      "third-party-mcp": "unreadable",
      "secret-block": "unreadable",
    });
  });

  it("does not trust retained data from an errored query", () => {
    queryHooks.activity.mockReturnValue({
      data: { activity: [{ targetId: "server-slug", totalToolCalls: 1 }] },
      isError: true,
      isPending: false,
    });
    queryHooks.results.mockReturnValue({
      data: { results: [{ id: "result-1" }] },
      isError: true,
      isPending: false,
    });

    const { result } = renderHook(() => useProjectGuideProgress());

    expect(result.current.statusByJourney).toEqual({
      "third-party-mcp": "unreadable",
      "secret-block": "unreadable",
    });
  });

  it("scopes every generated progress query to the request project", () => {
    const { rerender } = renderHook(() => useProjectGuideProgress());

    expect(queryHooks.servers).toHaveBeenCalledWith(
      { gramProject: "request-project" },
      undefined,
      { throwOnError: false },
    );
    expect(queryHooks.policies).toHaveBeenCalledWith(
      { gramProject: "request-project" },
      undefined,
      { throwOnError: false },
    );
    expect(queryHooks.plugins).toHaveBeenCalledWith(
      { gramProject: "request-project" },
      undefined,
      { throwOnError: false },
    );
    expect(queryHooks.remoteServers).toHaveBeenCalledWith(
      { gramProject: "request-project" },
      undefined,
      { throwOnError: false },
    );
    expect(queryHooks.activity).toHaveBeenCalledWith(
      { gramProject: "request-project", getMcpServerActivityPayload: {} },
      undefined,
      { throwOnError: false },
    );
    expect(queryHooks.results).toHaveBeenCalledWith(
      {
        gramProject: "request-project",
        category: "secrets",
        limit: 200,
      },
      undefined,
      { throwOnError: false },
    );

    requestProject.slug = "other-project";
    rerender();

    for (const hook of [
      queryHooks.servers,
      queryHooks.policies,
      queryHooks.plugins,
      queryHooks.remoteServers,
      queryHooks.activity,
      queryHooks.results,
    ]) {
      expect(hook).toHaveBeenLastCalledWith(
        expect.objectContaining({ gramProject: "other-project" }),
        undefined,
        { throwOnError: false },
      );
    }
  });
});
