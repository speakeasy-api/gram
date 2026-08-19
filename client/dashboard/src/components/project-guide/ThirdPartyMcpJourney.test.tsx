import {
  cleanup,
  fireEvent,
  render as baseRender,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PulseMCPServer } from "@/pages/catalog/hooks";
import { ConfigProvider } from "@/components/ui/context/ConfigContext";
import type { ExternalMCPRemote } from "@gram/client/models/components/externalmcpremote.js";
import { MemoryRouter } from "react-router";

const catalog = vi.hoisted(() => ({
  current: [] as PulseMCPServer[],
  isError: false,
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
  activity: vi.fn(),
  endpoints: vi.fn(),
  plugins: vi.fn(),
  remoteServers: vi.fn(),
  servers: vi.fn(),
}));

vi.mock("@/pages/catalog/hooks", () => ({
  useListMCPCatalog: () => ({
    data: { servers: catalog.current },
    isPending: false,
    isError: catalog.isError,
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

vi.mock("@gram/client/react-query/getMcpServerActivity.js", () => ({
  useGetMcpServerActivity: queries.activity,
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

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: { x: { overview: { href: (server: string) => `/mcp/${server}` } } },
  }),
}));

import { ThirdPartyMcpJourney } from "./ThirdPartyMcpJourney";

function noop(): void {}

function TestProviders({ children }: { children: React.ReactNode }) {
  return (
    <ConfigProvider theme="light" setTheme={noop}>
      <MemoryRouter>{children}</MemoryRouter>
    </ConfigProvider>
  );
}

function render(ui: React.ReactNode) {
  return baseRender(ui, { wrapper: TestProviders });
}

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

function setVerifiedServer() {
  const catalogEntry = server("Linear");
  catalog.current = [catalogEntry];
  queries.remoteServers.mockReturnValue({
    data: {
      remoteMcpServers: [
        { id: "remote-mcp", url: catalogEntry.remotes?.[0]?.url },
      ],
    },
    isPending: false,
    refetch: vi.fn(),
  });
  queries.servers.mockReturnValue({
    data: {
      mcpServers: [
        {
          id: "mcp-server",
          name: "Linear",
          remoteMcpServerId: "remote-mcp",
          slug: "linear",
        },
      ],
    },
    isPending: false,
    refetch: vi.fn(),
  });
  queries.endpoints.mockReturnValue({
    data: { mcpEndpoints: [{ mcpServerId: "mcp-server", slug: "linear" }] },
    isPending: false,
    refetch: vi.fn(),
  });
  queries.plugins.mockReturnValue({
    data: {
      plugins: [
        {
          isDefault: true,
          servers: [{ mcpServerId: "mcp-server" }],
        },
      ],
    },
    isPending: false,
    refetch: vi.fn(),
  });
}

beforeEach(() => {
  catalog.current = [];
  catalog.isError = false;
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
  queries.activity.mockReset();
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
  queries.activity.mockReturnValue({
    data: { activity: [] },
    isError: false,
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
  it("renders the approved five-step run with an activity panel", () => {
    catalog.current = [server("Linear")];

    render(
      <ThirdPartyMcpJourney
        status="not-started"
        onComplete={noop}
        onSwitchJourney={noop}
      />,
    );

    const steps = screen.getByRole("list", { name: "Journey A steps" });
    expect(steps.querySelectorAll(":scope > li")).toHaveLength(5);
    expect(screen.getByText("Pick a server from the catalog")).toBeTruthy();
    expect(screen.getByText("Confirm the governed endpoint")).toBeTruthy();
    expect(screen.getByText("Connect your client")).toBeTruthy();
    expect(screen.getByText("Ask the agent to list the tools")).toBeTruthy();
    expect(screen.getByText("Watch the first governed call")).toBeTruthy();
    expect(
      screen.getByRole("log", { name: "Journey A activity" }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Start the journey" }),
    ).toBeTruthy();
  });

  it("shows only automatic Pulse servers with HTTP remotes in preferred order", () => {
    catalog.current = [
      server("Other"),
      server("Ramp"),
      server("Notion"),
      server("Manual", { supportsDcr: false }),
      server("Vercel"),
      server("Linear"),
      server("SSE only", { remotes: ["sse"] }),
      { ...server("Mutating"), isReadOnly: false },
      server("Granola"),
      {
        ...server("Not a Pulse entry"),
        meta: undefined,
      } as unknown as PulseMCPServer,
    ];

    render(
      <ThirdPartyMcpJourney
        status="not-started"
        onComplete={noop}
        onSwitchJourney={noop}
      />,
    );

    expect(
      screen
        .getAllByTestId("project-guide-catalog-server")
        .map((button) => button.textContent),
    ).toEqual([
      "Linear1 tools",
      "Notion1 tools",
      "Vercel1 tools",
      "Granola1 tools",
      "Ramp1 tools",
    ]);
    fireEvent.click(
      screen.getByRole("button", { name: "More automatic servers" }),
    );
    expect(screen.getByRole("button", { name: /Other/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Manual/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /SSE only/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Mutating/ })).toBeNull();
  });

  it("skips selection for a resumed catalog-backed journey", () => {
    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={noop}
        onSwitchJourney={noop}
      />,
    );

    expect(screen.getByText("Deploy your server")).toBeTruthy();
    expect(
      screen
        .getByText("Confirm the governed endpoint")
        .closest("li")
        ?.getAttribute("aria-current"),
    ).toBe("step");
  });

  it("shows a retry state when no automatic catalog server is available", () => {
    render(
      <ThirdPartyMcpJourney
        status="not-started"
        onComplete={noop}
        onSwitchJourney={noop}
      />,
    );

    expect(
      screen.getByText("No automatic servers are available right now."),
    ).toBeTruthy();
    expect(screen.queryByText(/deployed/i)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry catalog" }));
    expect(catalog.refetch).toHaveBeenCalledOnce();
  });

  it("keeps the workflow servers array stable before a server is selected", () => {
    const rendered = render(
      <ThirdPartyMcpJourney
        status="not-started"
        onComplete={noop}
        onSwitchJourney={noop}
      />,
    );
    const firstServers = (
      workflowOptions.current as { servers: PulseMCPServer[] }
    ).servers;

    rendered.rerender(
      <ThirdPartyMcpJourney
        status="not-started"
        onComplete={noop}
        onSwitchJourney={noop}
      />,
    );

    expect(
      (workflowOptions.current as { servers: PulseMCPServer[] }).servers,
    ).toBe(firstServers);
  });

  it("advances to deployment and auto-selects every HTTP remote", () => {
    const selected = server("Linear", {
      remotes: ["streamable-http", "streamable-http"],
    });
    catalog.current = [selected];
    render(
      <ThirdPartyMcpJourney
        status="not-started"
        onComplete={noop}
        onSwitchJourney={noop}
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
        onComplete={noop}
        onSwitchJourney={noop}
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
        onComplete={noop}
        onSwitchJourney={noop}
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
        onComplete={noop}
        onSwitchJourney={noop}
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
          onComplete={noop}
          onSwitchJourney={noop}
        />,
      );

      await waitFor(() => {
        const row = screen.getByText("Pending server").closest("li");
        expect(row?.getAttribute("style") ?? "").not.toContain("opacity: 0");
        expect(
          row?.closest("section")?.getAttribute("style") ?? "",
        ).not.toContain("opacity: 0");
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
        onComplete={noop}
        onSwitchJourney={noop}
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
        onComplete={noop}
        onSwitchJourney={noop}
      />,
    );

    await waitFor(() => {
      expect(refetchServers).toHaveBeenCalledOnce();
      expect(refetchEndpoints).toHaveBeenCalledOnce();
      expect(refetchPlugins).toHaveBeenCalledOnce();
    });
  });

  it("resumes an existing fully deployed catalog server without starting another install", () => {
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
        onComplete={noop}
        onSwitchJourney={noop}
      />,
    );

    expect(screen.getByText("Verify your connection")).toBeTruthy();
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
        onComplete={noop}
        onSwitchJourney={noop}
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
        onComplete={noop}
        onSwitchJourney={noop}
      />,
    );

    expect(screen.getByRole("alert").textContent).toContain(
      "Could not load this project's deployment state.",
    );
    expect(
      screen.getByRole("button", { name: "Retry deployment state" }),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Install server" })).toBeNull();
    expect(screen.queryByText("Installed as a remote MCP server")).toBeNull();
  });

  it("resumes a fully deployed catalog server at verification after reload", () => {
    setVerifiedServer();

    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={noop}
        onSwitchJourney={noop}
      />,
    );

    expect(screen.getByText("Verify your connection")).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Claude" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Install server" })).toBeNull();
  });

  it("renders client configs, a safe prompt, and the governed endpoint", async () => {
    setVerifiedServer();

    render(
      <ThirdPartyMcpJourney
        status="done"
        onComplete={noop}
        onSwitchJourney={noop}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText(/"mcpServers"/)).toBeTruthy();
      expect(screen.getByText(/"linear"/)).toBeTruthy();
      expect(screen.getByText('"url"')).toBeTruthy();
      expect(screen.getByText('"url"').closest("pre")?.textContent).toContain(
        "/mcp/linear",
      );
    });
    expect(screen.getByRole("tablist", { name: "MCP client" })).toBeTruthy();
    fireEvent.mouseDown(screen.getByRole("tab", { name: "Cursor" }));
    await waitFor(() => expect(screen.getByText(/"mcpServers"/)).toBeTruthy());
    fireEvent.mouseDown(screen.getByRole("tab", { name: "Codex" }));
    await waitFor(() => {
      expect(screen.getByText("mcp_servers").closest("pre")?.textContent).toBe(
        '[mcp_servers.linear]url = "/mcp/linear"',
      );
    });
    expect(
      screen
        .getByRole("link", { name: "View Linear MCP server" })
        .getAttribute("href"),
    ).toMatch(/\/mcp\/linear$/);
    expect(screen.queryByText("mcp-server")).toBeNull();
    expect(
      (
        screen.getByRole("button", {
          name: "I've connected it",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);

    fireEvent.click(screen.getByText("mcp_servers").closest("pre")!);

    fireEvent.click(screen.getByRole("button", { name: "I've connected it" }));

    await waitFor(() =>
      expect(screen.getByText(/First list the available tools/)).toBeTruthy(),
    );
    expect(
      screen.getByText(/call one tool explicitly described as read-only/),
    ).toBeTruthy();
    expect(
      screen.getByText(
        /Do not use a tool that creates, updates, deletes, sends, or triggers anything/,
      ),
    ).toBeTruthy();
    expect(
      (screen.getByRole("button", { name: "Sent it" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    fireEvent.click(
      screen.getByText(/First list the available tools/).closest("pre")!,
    );
    expect(
      (screen.getByRole("button", { name: "Sent it" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);
    expect(queries.activity).toHaveBeenCalledWith(
      { gramProject: "project-guide-test", getMcpServerActivityPayload: {} },
      undefined,
      expect.objectContaining({ enabled: true, throwOnError: false }),
    );
  });

  it("restores an in-progress journey from matching activity", async () => {
    setVerifiedServer();
    workflow.current = {
      phase: "complete",
      reset: vi.fn(),
      statuses: [
        {
          key: "completed",
          mcpServerId: "mcp-server",
          name: "Linear",
          status: "completed",
        },
      ],
    };
    queries.activity.mockReturnValue({
      data: {
        activity: [
          {
            lastToolCallAt: new Date("2026-08-18T00:00:00.000Z"),
            recentToolCalls: 1,
            targetId: "linear",
            targetLabel: "List issues",
            targetType: "hosted_mcp_server",
            totalToolCalls: 1,
          },
        ],
      },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    const onComplete = vi.fn();

    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={() => {
          onComplete();
        }}
        onSwitchJourney={noop}
      />,
    );

    await waitFor(() => expect(onComplete).toHaveBeenCalledOnce());
    expect(screen.getByText("Journey A complete")).toBeTruthy();
    expect(screen.getByText("The path is governed.")).toBeTruthy();
    expect(screen.getByText("Governed call")).toBeTruthy();
    expect(screen.getByText("List issues")).toBeTruthy();
  });

  it("honors backend done when a matching activity is present", async () => {
    setVerifiedServer();
    queries.activity.mockReturnValue({
      data: {
        activity: [
          {
            lastToolCallAt: new Date("2026-08-18T00:00:00.000Z"),
            recentToolCalls: 1,
            targetId: "linear",
            targetLabel: "List issues",
            targetType: "hosted_mcp_server",
            totalToolCalls: 1,
          },
        ],
      },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    const onComplete = vi.fn();

    render(
      <ThirdPartyMcpJourney
        status="done"
        onComplete={() => {
          onComplete();
        }}
        onSwitchJourney={noop}
      />,
    );

    await waitFor(() => expect(onComplete).toHaveBeenCalledOnce());
    expect(screen.getByText("Journey A complete")).toBeTruthy();
  });

  it("does not restore from unrelated activity", async () => {
    setVerifiedServer();
    workflow.current = {
      phase: "complete",
      reset: vi.fn(),
      statuses: [
        {
          key: "completed",
          mcpServerId: "mcp-server",
          name: "Linear",
          status: "completed",
        },
      ],
    };
    queries.activity.mockReturnValue({
      data: {
        activity: [
          {
            lastToolCallAt: new Date("2026-08-18T00:00:00.000Z"),
            recentToolCalls: 1,
            targetId: "other-server",
            targetLabel: "List issues",
            targetType: "hosted_mcp_server",
            totalToolCalls: 1,
          },
        ],
      },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    const onComplete = vi.fn();

    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={() => {
          onComplete();
        }}
        onSwitchJourney={noop}
      />,
    );

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: "I've connected it" }),
      ).toBeTruthy();
    });
    expect(onComplete).not.toHaveBeenCalled();
  });

  it("does not credit a call recorded before a new prompt starts", async () => {
    setVerifiedServer();
    workflow.current = {
      phase: "complete",
      reset: vi.fn(),
      statuses: [
        {
          key: "completed",
          mcpServerId: "mcp-server",
          name: "Linear",
          status: "completed",
        },
      ],
    };
    const activity = { current: [] as Array<Record<string, unknown>> };
    queries.activity.mockImplementation(() => ({
      data: { activity: activity.current },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    }));
    const onComplete = vi.fn();

    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={() => {
          onComplete();
        }}
        onSwitchJourney={noop}
      />,
    );

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: "I've connected it" }),
      ).toBeTruthy();
    });
    await waitFor(() => expect(screen.getByText('"url"')).toBeTruthy());
    fireEvent.click(screen.getByText('"url"').closest("pre")!);
    fireEvent.click(screen.getByRole("button", { name: "I've connected it" }));
    await waitFor(() =>
      expect(screen.getByText(/Using the Linear MCP server/)).toBeTruthy(),
    );
    fireEvent.click(
      screen.getByText(/Using the Linear MCP server/).closest("pre")!,
    );
    fireEvent.click(screen.getByRole("button", { name: "Sent it" }));
    activity.current = [
      {
        lastToolCallAt: new Date("2020-01-01T00:00:00.000Z"),
        recentToolCalls: 1,
        targetId: "linear",
        targetLabel: "List issues",
        targetType: "hosted_mcp_server",
        totalToolCalls: 1,
      },
    ];

    expect(onComplete).not.toHaveBeenCalled();
    expect(
      screen.getByText("Listening for the first call on your endpoint"),
    ).toBeTruthy();
  });

  it("captures the server activity baseline only when listening starts", async () => {
    setVerifiedServer();
    const activity = { current: [] as Array<Record<string, unknown>> };
    queries.activity.mockImplementation(() => ({
      data: { activity: activity.current },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    }));
    const onComplete = vi.fn();
    const rendered = render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={() => {
          onComplete();
        }}
        onSwitchJourney={noop}
      />,
    );

    await waitFor(() => expect(screen.getByText('"url"')).toBeTruthy());
    fireEvent.click(screen.getByText('"url"').closest("pre")!);
    fireEvent.click(screen.getByRole("button", { name: "I've connected it" }));
    await waitFor(() =>
      expect(screen.getByText(/First list the available tools/)).toBeTruthy(),
    );

    activity.current = [
      {
        lastToolCallAt: new Date("2026-08-19T12:00:00Z"),
        recentToolCalls: 1,
        targetId: "linear",
        targetLabel: "Intervening read",
        targetType: "hosted_mcp_server",
        totalToolCalls: 1,
      },
    ];
    rendered.rerender(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={() => {
          onComplete();
        }}
        onSwitchJourney={noop}
      />,
    );
    fireEvent.click(
      screen.getByText(/First list the available tools/).closest("pre")!,
    );
    fireEvent.click(screen.getByRole("button", { name: "Sent it" }));

    expect(onComplete).not.toHaveBeenCalled();
    expect(
      screen.getByText("Listening for the first call on your endpoint"),
    ).toBeTruthy();

    activity.current = [
      {
        lastToolCallAt: new Date("2026-08-19T12:00:01Z"),
        recentToolCalls: 2,
        targetId: "linear",
        targetLabel: "Safe read",
        targetType: "hosted_mcp_server",
        totalToolCalls: 2,
      },
    ];
    rendered.rerender(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={() => {
          onComplete();
        }}
        onSwitchJourney={noop}
      />,
    );

    await waitFor(() => expect(onComplete).toHaveBeenCalledOnce());
  });

  it("pauses and resumes only the live activity check", async () => {
    setVerifiedServer();

    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={noop}
        onSwitchJourney={noop}
      />,
    );

    await waitFor(() => expect(screen.getByText('"url"')).toBeTruthy());
    fireEvent.click(screen.getByText('"url"').closest("pre")!);
    fireEvent.click(screen.getByRole("button", { name: "I've connected it" }));
    await waitFor(() =>
      expect(screen.getByText(/First list the available tools/)).toBeTruthy(),
    );
    fireEvent.click(
      screen.getByText(/First list the available tools/).closest("pre")!,
    );
    fireEvent.click(screen.getByRole("button", { name: "Sent it" }));
    fireEvent.click(screen.getByRole("button", { name: "Pause live checks" }));

    expect(
      screen.getByRole("button", { name: "Resume live checks" }),
    ).toBeTruthy();
    expect(queries.activity.mock.calls.at(-1)?.[2]).toMatchObject({
      enabled: false,
    });
    expect(workflow.current).toMatchObject({ phase: "configure" });

    fireEvent.click(screen.getByRole("button", { name: "Resume live checks" }));
    expect(queries.activity.mock.calls.at(-1)?.[2]).toMatchObject({
      enabled: true,
    });
  });

  it("completes a new run only after a governed call arrives", async () => {
    setVerifiedServer();
    workflow.current = {
      phase: "complete",
      reset: vi.fn(),
      statuses: [
        {
          key: "completed",
          mcpServerId: "mcp-server",
          name: "Linear",
          status: "completed",
        },
      ],
    };
    const activity = { current: [] as Array<Record<string, unknown>> };
    queries.activity.mockImplementation(() => ({
      data: { activity: activity.current },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    }));
    const onComplete = vi.fn();
    const rendered = render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={() => {
          onComplete();
        }}
        onSwitchJourney={noop}
      />,
    );

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: "I've connected it" }),
      ).toBeTruthy();
    });
    fireEvent.click(screen.getByText('"url"').closest("pre")!);
    fireEvent.click(screen.getByRole("button", { name: "I've connected it" }));
    await waitFor(() =>
      expect(screen.getByText(/Using the Linear MCP server/)).toBeTruthy(),
    );
    fireEvent.click(
      screen.getByText(/Using the Linear MCP server/).closest("pre")!,
    );
    fireEvent.click(screen.getByRole("button", { name: "Sent it" }));
    activity.current = [
      {
        lastToolCallAt: new Date(Date.now() + 60_000),
        recentToolCalls: 1,
        targetId: "linear",
        targetLabel: "List issues",
        targetType: "hosted_mcp_server",
        totalToolCalls: 2,
      },
    ];
    rendered.rerender(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={() => {
          onComplete();
        }}
        onSwitchJourney={noop}
      />,
    );

    await waitFor(() => expect(onComplete).toHaveBeenCalledOnce());
  });

  it("keeps the journey incomplete when activity cannot be checked", () => {
    setVerifiedServer();
    const refetch = vi.fn();
    queries.activity.mockReturnValue({
      data: undefined,
      isError: true,
      isPending: false,
      refetch,
    });
    const onComplete = vi.fn();

    render(
      <ThirdPartyMcpJourney
        status="done"
        onComplete={() => {
          onComplete();
        }}
        onSwitchJourney={noop}
      />,
    );

    expect(onComplete).not.toHaveBeenCalled();
    expect(
      screen.getByText("Could not check for the first governed call."),
    ).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: "Retry activity check" }),
    );
    expect(refetch).toHaveBeenCalledOnce();
  });

  it("scopes deployment workflow and queries to the request project", () => {
    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={noop}
        onSwitchJourney={noop}
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
    expect(queries.activity).toHaveBeenCalledWith(
      { gramProject: "project-guide-test", getMcpServerActivityPayload: {} },
      undefined,
      expect.objectContaining({ throwOnError: false }),
    );
  });
});
