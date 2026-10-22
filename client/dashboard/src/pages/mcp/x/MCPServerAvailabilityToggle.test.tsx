import { unrecognized } from "@gram/client/types/unrecognized.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  MCPServerAvailabilityToggle,
  MCPServerStatusDropdown,
} from "./MCPServerDetails";

const mocks = vi.hoisted(() => ({
  hasScope: vi.fn(),
  guardRequest: vi.fn(),
  mutate: vi.fn(),
  mutationOptions: vi.fn(),
  invalidateAudience: vi.fn(() => Promise.resolve()),
  tunneledSource: vi.fn(),
}));

vi.mock("@gram/client/react-query/resourceAudience.js", () => ({
  invalidateAllResourceAudience: mocks.invalidateAudience,
}));

vi.mock("./hosted/useHostedVisibilityGuard", () => ({
  useHostedVisibilityGuard: ({
    toolsetSlug,
    apply,
  }: {
    toolsetSlug?: string;
    apply: (next: string) => void;
  }) => ({
    request: (next: string) => {
      mocks.guardRequest(toolsetSlug, next);
      apply(next);
    },
    publicUnavailable: false,
    dialogs: null,
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: mocks.hasScope }),
}));
vi.mock("@gram/client/react-query/updateMcpServer.js", () => ({
  useUpdateMcpServerMutation: (options: unknown) => {
    mocks.mutationOptions(options);
    return { isPending: false, mutate: mocks.mutate };
  },
}));
vi.mock("@gram/client/react-query/getTunneledMcpServer.js", () => ({
  useGetTunneledMcpServer: () => mocks.tunneledSource(),
}));
vi.mock("@/components/ui/Dropdown", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ children }: { children: ReactNode }) => (
    <>{children}</>
  ),
  DropdownMenuContent: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuItem: ({
    children,
    disabled,
    onSelect,
  }: {
    children: ReactNode;
    disabled?: boolean;
    onSelect?: () => void;
  }) => (
    <div
      role="menuitem"
      data-disabled={disabled ? "" : undefined}
      onClick={disabled ? undefined : onSelect}
    >
      {children}
    </div>
  ),
}));

const server = {
  id: "mcp-server-1",
  projectId: "project-1",
  name: "Example server",
  networkAccessMode: "public_only",
  remoteMcpServerId: "remote-source-1",
  toolVariationsGroupId: "tool-filter-1",
  visibility: "private",
  createdAt: new Date(0),
  updatedAt: new Date(0),
} as McpServer;

// The status dropdown links to the source's public-access section, so these
// renders need a router as well as the query client.
function renderInApp(ui: ReactNode): void {
  render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>{ui}</QueryClientProvider>
    </MemoryRouter>,
  );
}

function renderToggle(mcpServer: McpServer = server): void {
  renderInApp(<MCPServerAvailabilityToggle server={mcpServer} />);
}

beforeEach(() => {
  mocks.hasScope.mockReturnValue(true);
  mocks.tunneledSource.mockReturnValue({ data: { allowPublic: false } });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("MCPServerAvailabilityToggle", () => {
  it("keeps unknown availability neutral and blocks both controls", () => {
    const unknown = {
      ...server,
      visibility: unrecognized("future_visibility"),
    };
    renderInApp(
      <>
        <MCPServerAvailabilityToggle server={unknown} />
        <MCPServerStatusDropdown server={unknown} />
      </>,
    );
    const toggle = screen.getByRole("switch", {
      name: "MCP server availability unknown",
    });
    const dropdown = screen.getByRole("button", { name: "Unknown" });
    expect(toggle).toHaveProperty("disabled", true);
    expect(toggle.getAttribute("aria-checked")).toBe("false");
    expect(toggle.parentElement?.getAttribute("title")).toContain(
      "Refresh the page",
    );
    expect(dropdown).toHaveProperty("disabled", true);
    fireEvent.click(toggle);
    fireEvent.click(dropdown);
    fireEvent.click(screen.getByRole("menuitem", { name: /^Private/ }));
    expect(mocks.mutate).not.toHaveBeenCalled();
  });

  it("disables a Remote MCP server while preserving its backend fields", () => {
    renderToggle();

    fireEvent.click(screen.getByRole("switch", { name: "Disable MCP server" }));

    expect(mocks.mutate).toHaveBeenCalledWith({
      request: {
        updateMcpServerForm: {
          id: "mcp-server-1",
          name: "Example server",
          remoteMcpServerId: "remote-source-1",
          tunneledMcpServerId: undefined,
          toolsetId: undefined,
          unproxiedMcpServerId: undefined,
          environmentId: undefined,
          toolVariationsGroupId: "tool-filter-1",
          visibility: "disabled",
        },
      },
    });
  });

  it("enables a disabled server as private", () => {
    renderToggle({ ...server, visibility: "disabled" });

    fireEvent.click(screen.getByRole("switch", { name: "Enable MCP server" }));

    expect(mocks.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          updateMcpServerForm: expect.objectContaining({
            visibility: "private",
          }),
        }),
      }),
    );
  });

  it.each(["private", "disabled"] as const)(
    "refreshes team audience after changing visibility from %s",
    async (visibility) => {
      renderToggle({ ...server, visibility });
      fireEvent.click(screen.getByRole("switch"));
      expect(mocks.invalidateAudience).not.toHaveBeenCalled();

      const [variables] = mocks.mutate.mock.calls.at(-1)!;
      const [options] = mocks.mutationOptions.mock.calls.at(-1)!;
      await options.onSuccess(undefined, variables);

      expect(mocks.invalidateAudience).toHaveBeenCalledExactlyOnceWith(
        expect.any(QueryClient),
        { refetchType: "all" },
      );
    },
  );

  it("preserves the existing write-scope gate", () => {
    mocks.hasScope.mockReturnValue(false);
    renderToggle();

    expect(
      (
        screen.getByRole("switch", {
          name: "Disable MCP server",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("keeps Public blocked until a tunneled source opts in", () => {
    renderInApp(
      <MCPServerStatusDropdown
        server={{
          ...server,
          remoteMcpServerId: undefined,
          tunneledMcpServerId: "tunneled-source-1",
        }}
      />,
    );

    expect(
      screen.getByText(
        "Enable public access on the tunnel source first to allow anonymous serving.",
      ),
    ).toBeDefined();
    expect(
      screen
        .getByRole("menuitem", { name: /^Public/ })
        .getAttribute("data-disabled"),
    ).not.toBeNull();
  });

  it("keeps the tunneled Public option after the source opts in", () => {
    mocks.tunneledSource.mockReturnValue({ data: { allowPublic: true } });
    renderInApp(
      <MCPServerStatusDropdown
        server={{
          ...server,
          remoteMcpServerId: undefined,
          tunneledMcpServerId: "tunneled-source-1",
        }}
      />,
    );

    fireEvent.click(screen.getByRole("menuitem", { name: /^Public/ }));

    expect(mocks.mutate).toHaveBeenCalledWith({
      request: {
        updateMcpServerForm: expect.objectContaining({
          tunneledMcpServerId: "tunneled-source-1",
          visibility: "public",
        }),
      },
    });
  });
});

describe("hosted MCP server visibility", () => {
  const hosted = {
    ...server,
    id: "hosted-1",
    remoteMcpServerId: undefined,
    toolsetId: "hosted-1",
    toolset: {
      id: "hosted-1",
      slug: "hosted-tools",
      name: "Hosted tools",
      toolCount: 1,
      toolSelectionMode: "static",
      externalMcpSlugs: [],
      externalMcpProxyOnly: false,
    },
    visibility: "private",
  } as McpServer;

  it("lets the availability switch disable a hosted server", () => {
    renderToggle(hosted);

    fireEvent.click(screen.getByRole("switch", { name: "Disable MCP server" }));

    expect(mocks.mutate).toHaveBeenCalledWith({
      request: {
        updateMcpServerForm: expect.objectContaining({
          id: "hosted-1",
          toolsetId: "hosted-1",
          visibility: "disabled",
        }),
      },
    });
  });

  it("offers Public through the hosted confirmations", () => {
    renderInApp(<MCPServerStatusDropdown server={hosted} />);

    fireEvent.click(screen.getByRole("menuitem", { name: /^Public/ }));

    expect(mocks.guardRequest).toHaveBeenCalledWith("hosted-tools", "public");
    expect(mocks.mutate).toHaveBeenCalledWith({
      request: {
        updateMcpServerForm: expect.objectContaining({
          id: "hosted-1",
          visibility: "public",
        }),
      },
    });
  });
});

it("keeps public non-tunneled servers green without offering Public as an editable option", () => {
  renderInApp(
    <MCPServerStatusDropdown server={{ ...server, visibility: "public" }} />,
  );
  const current = screen.getByRole("button", { name: "Public" });
  expect(current.querySelector(".bg-green-400")).not.toBeNull();
  expect(screen.queryByRole("menuitem", { name: /^Public/ })).toBeNull();
});
