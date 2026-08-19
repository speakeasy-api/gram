import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PulseMCPServer } from "@/pages/catalog/hooks";
import type { ExternalMCPRemote } from "@gram/client/models/components/externalmcpremote.js";

const catalog = vi.hoisted(() => ({
  current: [] as PulseMCPServer[],
  refetch: vi.fn(),
}));
const workflowOptions = vi.hoisted(() => ({ current: undefined as unknown }));
const workflow = vi.hoisted(() => ({
  current: {
    canInstall: true,
    phase: "configure",
    reset: vi.fn(),
    startInstall: vi.fn().mockResolvedValue(undefined),
  } as unknown,
}));
const queries = vi.hoisted(() => ({
  endpoints: vi.fn(),
  plugins: vi.fn(),
  remoteServers: vi.fn(),
  servers: vi.fn(),
}));

vi.mock("@/pages/catalog/hooks", () => ({
  useListMCPCatalog: () => ({
    data: { servers: catalog.current },
    isPending: false,
    isError: false,
    refetch: catalog.refetch,
  }),
}));

vi.mock("@/pages/catalog/useRemoteMcpInstallWorkflow", () => ({
  useRemoteMcpInstallWorkflow: (options: unknown) => {
    workflowOptions.current = options;
    return workflow.current;
  },
}));

vi.mock("@gram/client/react-query/mcpEndpoints.js", () => ({
  useMcpEndpoints: queries.endpoints,
}));

vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: queries.servers,
}));

vi.mock("@gram/client/react-query/plugins.js", () => ({
  usePlugins: queries.plugins,
}));

vi.mock("@gram/client/react-query/remoteMcpServers.js", () => ({
  useRemoteMcpServers: queries.remoteServers,
}));

vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project-guide-test",
}));

import { ThirdPartyMcpJourney } from "./ThirdPartyMcpJourney";

function server(
  title: string,
  {
    supportsDcr = true,
    remotes = ["streamable-http"],
  }: {
    supportsDcr?: boolean;
    remotes?: ExternalMCPRemote["transportType"][];
  } = {},
): PulseMCPServer {
  return {
    registryId: "catalog",
    registrySpecifier: `example/${title.toLowerCase()}`,
    title,
    description: "Catalog server",
    version: "1.0.0",
    meta: {},
    toolCount: 1,
    isReadOnly: true,
    supportsDcr,
    remotes: remotes.map((transportType, index) => ({
      url: `https://example.test/${title}/${index}`,
      transportType,
    })),
  };
}

beforeEach(() => {
  catalog.current = [];
  catalog.refetch.mockClear();
  workflowOptions.current = undefined;
  workflow.current = {
    canInstall: true,
    phase: "configure",
    reset: vi.fn(),
    startInstall: vi.fn().mockResolvedValue(undefined),
  };
  queries.servers.mockReset();
  queries.endpoints.mockReset();
  queries.plugins.mockReset();
  queries.remoteServers.mockReset();
  queries.servers.mockReturnValue({
    data: { mcpServers: [] },
    isPending: false,
    refetch: vi.fn(),
  });
  queries.endpoints.mockReturnValue({
    data: { mcpEndpoints: [] },
    isPending: false,
    refetch: vi.fn(),
  });
  queries.plugins.mockReturnValue({
    data: { plugins: [] },
    isPending: false,
    refetch: vi.fn(),
  });
  queries.remoteServers.mockReturnValue({
    data: { remoteMcpServers: [] },
    isPending: false,
    refetch: vi.fn(),
  });
});

afterEach(() => {
  cleanup();
});

describe("ThirdPartyMcpJourney", () => {
  it("shows only automatic Pulse servers with HTTP remotes in preferred order", () => {
    catalog.current = [
      server("Other"),
      server("Ramp"),
      server("Notion"),
      server("Manual", { supportsDcr: false }),
      server("Vercel"),
      server("Linear"),
      server("SSE only", { remotes: ["sse"] }),
      server("Granola"),
      {
        ...server("Not a Pulse entry"),
        meta: undefined,
      } as unknown as PulseMCPServer,
    ];

    render(
      <ThirdPartyMcpJourney
        status="not-started"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    expect(
      screen
        .getAllByRole("button")
        .filter((button) => button.textContent !== "Switch journey")
        .map((button) => button.textContent),
    ).toEqual([
      "Linear1 tools",
      "Notion1 tools",
      "Vercel1 tools",
      "Granola1 tools",
      "Ramp1 tools",
      "More automatic servers",
    ]);
    fireEvent.click(
      screen.getByRole("button", { name: "More automatic servers" }),
    );
    expect(screen.getByRole("button", { name: /Other/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Manual/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /SSE only/ })).toBeNull();
  });

  it("skips selection for a resumed catalog-backed journey", () => {
    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    expect(screen.getByText("Deploy your server")).toBeTruthy();
    expect(screen.queryByText("Pick a server from the catalog")).toBeNull();
  });

  it("shows a retry state when no automatic catalog server is available", () => {
    render(
      <ThirdPartyMcpJourney
        status="not-started"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    expect(
      screen.getByText("No automatic servers are available right now."),
    ).toBeTruthy();
    expect(screen.queryByText(/deployed/i)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry catalog" }));
    expect(catalog.refetch).toHaveBeenCalledOnce();
  });

  it("advances to deployment and auto-selects every HTTP remote", () => {
    const selected = server("Linear", {
      remotes: ["streamable-http", "streamable-http"],
    });
    catalog.current = [selected];
    render(
      <ThirdPartyMcpJourney
        status="not-started"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /Linear/ }));

    expect(screen.getByText("Deploy your server")).toBeTruthy();
    expect(workflowOptions.current).toMatchObject({
      servers: [selected],
      autoSelectRemotes: true,
      projectSlug: "project-guide-test",
    });
  });

  it("starts one automatic server installation from the deployment action", () => {
    const startInstall = vi.fn().mockResolvedValue(undefined);
    workflow.current = {
      canInstall: true,
      phase: "configure",
      reset: vi.fn(),
      startInstall,
    };
    catalog.current = [server("Linear")];

    render(
      <ThirdPartyMcpJourney
        status="not-started"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /Linear/ }));

    expect(screen.getByText("Read the server's tool list")).toBeTruthy();
    expect(screen.getByText("Install it into this project")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Install server" }));
    expect(startInstall).toHaveBeenCalledOnce();
  });

  it("advances to verification after a successful workflow completes", async () => {
    workflow.current = {
      phase: "complete",
      reset: vi.fn(),
      statuses: [
        {
          key: "completed",
          mcpServerId: "server-1",
          name: "Linear",
          status: "completed",
        },
      ],
    };

    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText("Verify your connection")).toBeTruthy();
    });
  });

  it("renders every installation status inline", () => {
    workflow.current = {
      phase: "installing",
      reset: vi.fn(),
      statuses: [
        { key: "pending", name: "Pending server", status: "pending" },
        { key: "creating", name: "Creating server", status: "creating" },
        { key: "completed", name: "Completed server", status: "completed" },
        { key: "failed", name: "Failed server", status: "failed" },
      ],
    };

    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    expect(screen.getByText("Waiting to install")).toBeTruthy();
    expect(screen.getByText("Creating remote MCP server")).toBeTruthy();
    expect(screen.getByText("Installed as a remote MCP server")).toBeTruthy();
    expect(screen.getByText("Install failed")).toBeTruthy();
    expect(screen.getByText("Pending server")).toBeTruthy();
    expect(screen.getByText("Creating server")).toBeTruthy();
  });

  it("renders deployment rows without animation when reduced motion is preferred", async () => {
    const originalMatchMedia = window.matchMedia;
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: vi.fn().mockReturnValue({
        addEventListener: vi.fn(),
        addListener: vi.fn(),
        dispatchEvent: vi.fn(),
        matches: true,
        media: "(prefers-reduced-motion: reduce)",
        onchange: null,
        removeEventListener: vi.fn(),
        removeListener: vi.fn(),
      }),
    });
    workflow.current = {
      phase: "installing",
      reset: vi.fn(),
      statuses: [{ key: "pending", name: "Pending server", status: "pending" }],
    };

    try {
      render(
        <ThirdPartyMcpJourney
          status="in-progress"
          onComplete={vi.fn()}
          onSwitchJourney={vi.fn()}
        />,
      );

      await waitFor(() => {
        const row = screen.getByText("Pending server").closest("li");
        expect(row?.getAttribute("style") ?? "").not.toContain("opacity: 0");
      });
    } finally {
      Object.defineProperty(window, "matchMedia", {
        configurable: true,
        value: originalMatchMedia,
      });
    }
  });

  it("offers retry after a failed install without advancing", () => {
    const reset = vi.fn();
    workflow.current = {
      phase: "complete",
      reset,
      statuses: [
        {
          error: "The upstream did not answer.",
          key: "failed",
          name: "Failed server",
          status: "failed",
        },
      ],
    };

    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Retry installation" }));
    expect(reset).toHaveBeenCalledOnce();
    expect(screen.queryByText("Verify your connection")).toBeNull();
  });

  it("refreshes deployment queries when installation completes", async () => {
    const refetchServers = vi.fn();
    const refetchEndpoints = vi.fn();
    const refetchPlugins = vi.fn();
    workflow.current = {
      phase: "complete",
      reset: vi.fn(),
      statuses: [
        {
          key: "completed",
          mcpServerId: "server-1",
          name: "Linear",
          status: "completed",
        },
      ],
    };
    queries.servers.mockReturnValue({
      data: {
        mcpServers: [{ id: "server-1", remoteMcpServerId: "remote-1" }],
      },
      isPending: false,
      refetch: refetchServers,
    });
    queries.endpoints.mockReturnValue({
      data: { mcpEndpoints: [{ mcpServerId: "server-1", slug: "linear" }] },
      isPending: false,
      refetch: refetchEndpoints,
    });
    queries.plugins.mockReturnValue({
      data: {
        plugins: [
          {
            isDefault: true,
            servers: [{ mcpServerId: "server-1" }],
          },
        ],
      },
      isPending: false,
      refetch: refetchPlugins,
    });

    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    await waitFor(() => {
      expect(refetchServers).toHaveBeenCalledOnce();
      expect(refetchEndpoints).toHaveBeenCalledOnce();
      expect(refetchPlugins).toHaveBeenCalledOnce();
    });
  });

  it("resumes an existing catalog-backed server without starting another install", () => {
    const startInstall = vi.fn();
    const catalogEntry = server("Linear");
    workflow.current = {
      canInstall: true,
      phase: "configure",
      reset: vi.fn(),
      startInstall,
    };
    catalog.current = [catalogEntry];
    queries.remoteServers.mockReturnValue({
      data: {
        remoteMcpServers: [
          { id: "remote-1", url: catalogEntry.remotes?.[0]?.url },
        ],
      },
      isPending: false,
      refetch: vi.fn(),
    });
    queries.servers.mockReturnValue({
      data: {
        mcpServers: [{ id: "server-1", remoteMcpServerId: "remote-1" }],
      },
      isPending: false,
      refetch: vi.fn(),
    });
    queries.endpoints.mockReturnValue({
      data: { mcpEndpoints: [{ mcpServerId: "server-1", slug: "linear" }] },
      isPending: false,
      refetch: vi.fn(),
    });
    queries.plugins.mockReturnValue({
      data: {
        plugins: [
          {
            isDefault: true,
            servers: [{ mcpServerId: "server-1" }],
          },
        ],
      },
      isPending: false,
      refetch: vi.fn(),
    });

    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    expect(screen.getByText("Installed as a remote MCP server")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Install server" })).toBeNull();
    expect(startInstall).not.toHaveBeenCalled();
  });

  it("does not resume an unrelated remote MCP server", () => {
    catalog.current = [server("Linear")];
    queries.remoteServers.mockReturnValue({
      data: {
        remoteMcpServers: [
          { id: "remote-1", url: "https://unrelated.example/mcp" },
        ],
      },
      isPending: false,
      refetch: vi.fn(),
    });
    queries.servers.mockReturnValue({
      data: {
        mcpServers: [{ id: "server-1", remoteMcpServerId: "remote-1" }],
      },
      isPending: false,
      refetch: vi.fn(),
    });

    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "Install server" })).toBeTruthy();
    expect(screen.queryByText("Installed as a remote MCP server")).toBeNull();
  });

  it("does not resume from cached data when a project query is errored", () => {
    const catalogEntry = server("Linear");
    catalog.current = [catalogEntry];
    queries.remoteServers.mockReturnValue({
      data: {
        remoteMcpServers: [
          { id: "remote-1", url: catalogEntry.remotes?.[0]?.url },
        ],
      },
      isPending: false,
      refetch: vi.fn(),
    });
    queries.servers.mockReturnValue({
      data: {
        mcpServers: [{ id: "server-1", remoteMcpServerId: "remote-1" }],
      },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    queries.plugins.mockReturnValue({
      data: {
        plugins: [
          {
            isDefault: true,
            servers: [{ mcpServerId: "server-1" }],
          },
        ],
      },
      isError: true,
      isPending: false,
      refetch: vi.fn(),
    });

    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "Install server" })).toBeTruthy();
    expect(screen.queryByText("Installed as a remote MCP server")).toBeNull();
  });

  it("scopes deployment workflow and queries to the request project", () => {
    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    expect(workflowOptions.current).toMatchObject({
      projectSlug: "project-guide-test",
    });
    for (const query of [
      queries.servers,
      queries.endpoints,
      queries.plugins,
      queries.remoteServers,
    ]) {
      expect(query).toHaveBeenCalledWith(
        { gramProject: "project-guide-test" },
        undefined,
        { throwOnError: false },
      );
    }
  });
});
