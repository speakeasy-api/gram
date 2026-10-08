import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { RemoteMcpToolsSection } from "./RemoteMcpToolsSection";

const mocks = vi.hoisted(() => ({
  needsAuth: true,
  isError: false,
  tools: undefined as Record<string, { annotations?: object }> | undefined,
  metadataByTool: {} as Record<string, object>,
  tunnelOffline: false,
  open: vi.fn(),
  refetch: vi.fn(),
  tunnelRefetch: vi.fn(),
  syncArgs: [] as Array<{ mode: string; live: unknown; enabled: boolean }>,
  remove: vi.fn(),
}));

vi.mock("@/hooks/useProxiedMcpTools", () => ({
  useProxiedMcpTools: () => ({
    tools: mocks.tools,
    isLoading: false,
    needsAuth: mocks.needsAuth,
    isError: mocks.isError,
    refetch: mocks.refetch,
    error: null,
  }),
}));

vi.mock("./useTunnelAgentStatus", () => ({
  useTunnelAgentStatus: () => ({
    offline: mocks.tunnelOffline,
    refetch: mocks.tunnelRefetch,
  }),
}));

vi.mock("@/hooks/useUserSessionToken", () => ({
  useUserSessionToken: () => ({ accessToken: undefined, isLoading: false }),
}));

vi.mock("@/hooks/useToolMetadata", () => ({
  useToolMetadata: () => ({
    metadataByTool: mocks.metadataByTool,
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));

vi.mock("./useSyncToolMetadata", () => ({
  useSyncToolMetadata: (args: {
    mode: string;
    live: unknown;
    enabled: boolean;
  }) => {
    mocks.syncArgs.push(args);
    return args.mode === "additive"
      ? {
          sync: undefined,
          isSyncing: false,
          toolActions: {
            record: vi.fn(),
            apply: vi.fn(),
            remove: mocks.remove,
            pendingTool: undefined,
          },
        }
      : { sync: vi.fn(), isSyncing: false, toolActions: undefined };
  },
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
  }: {
    children: ReactNode | ((state: { disabled: boolean }) => ReactNode);
  }) =>
    typeof children === "function" ? children({ disabled: false }) : children,
}));

vi.mock("@/lib/utils", async () => {
  const actual =
    await vi.importActual<typeof import("@/lib/utils")>("@/lib/utils");
  return {
    ...actual,
    mcpConnectionUrl: (url: string | undefined) => url,
    getServerURL: () => "https://gram.example",
    firstPartyConnectUrl: (url: string | undefined) =>
      url ? `${url}/connect/first-party` : undefined,
  };
});

function renderSection(
  props: Partial<ComponentProps<typeof RemoteMcpToolsSection>> = {},
): void {
  render(
    <TooltipProvider>
      <MemoryRouter>
        <RemoteMcpToolsSection
          mcpUrl="https://mcp.example/mcp/custom-slug"
          isResolvingUrl={false}
          mcpServerId="mcp-server-1"
          userSessionIssuerId={undefined}
          isDisabled={false}
          authSettingsHref="/settings#authentication"
          platformSlug="server"
          {...props}
        />
      </MemoryRouter>
    </TooltipProvider>,
  );
}

describe("RemoteMcpToolsSection connect prompt", () => {
  afterEach(() => {
    cleanup();
    mocks.needsAuth = true;
    mocks.open.mockReset();
  });

  it("does not open first-party connect when the server has no issuer", () => {
    vi.stubGlobal("open", mocks.open);
    renderSection();

    expect(screen.queryByRole("button", { name: "Connect" })).toBeNull();
    expect(screen.getByText(/no authentication configured yet/i)).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Configure authentication" }),
    ).toBeTruthy();
    expect(mocks.open).not.toHaveBeenCalled();
  });

  it("opens the platform slug connect page for an issuer-gated server", () => {
    vi.stubGlobal("open", mocks.open);
    renderSection({ userSessionIssuerId: "issuer-1" });

    fireEvent.click(screen.getByRole("button", { name: "Connect" }));

    expect(mocks.open).toHaveBeenCalledWith(
      "https://gram.example/mcp/server/connect/first-party",
      "_blank",
      "noopener,noreferrer",
    );
  });

  it("does not offer connect for a public tunnel", () => {
    renderSection({
      userSessionIssuerId: "issuer-1",
      tunneledMcpServerId: "tunnel-1",
      visibility: "public",
    });

    expect(screen.queryByRole("button", { name: "Connect" })).toBeNull();
    expect(screen.getByText(/Connect isn't available/i)).toBeTruthy();
  });

  it("does not offer connect when only a custom-domain address exists", () => {
    renderSection({
      userSessionIssuerId: "issuer-1",
      platformSlug: undefined,
    });

    expect(screen.queryByRole("button", { name: "Connect" })).toBeNull();
    expect(screen.getByText(/Connect isn't available/i)).toBeTruthy();
  });
});

function storedTool(toolName: string, readOnlyHint?: boolean): object {
  return {
    mcpServerId: "mcp-server-1",
    toolName,
    readOnlyHint,
    createdAt: new Date("2026-01-01T00:00:00Z"),
    updatedAt: new Date("2026-01-01T00:00:00Z"),
  };
}

describe("RemoteMcpToolsSection tunneled servers", () => {
  afterEach(() => {
    cleanup();
    mocks.needsAuth = true;
    mocks.isError = false;
    mocks.tools = undefined;
    mocks.metadataByTool = {};
    mocks.tunnelOffline = false;
    mocks.syncArgs.length = 0;
    vi.clearAllMocks();
  });

  const tunneledProps = {
    userSessionIssuerId: "issuer-1",
    tunneledMcpServerId: "tunnel-1",
    visibility: "private",
  };

  it("shows the offline state and the recorded tools when the agent is offline", () => {
    mocks.needsAuth = false;
    mocks.isError = true;
    mocks.tunnelOffline = true;
    mocks.metadataByTool = {
      list_devices: storedTool("list_devices", true),
      wipe_device: storedTool("wipe_device"),
    };
    renderSection(tunneledProps);

    expect(
      screen.getByText("Tunnel offline — connect the agent to list its tools."),
    ).toBeTruthy();
    expect(screen.getByText("list_devices")).toBeTruthy();
    expect(screen.getByText("wipe_device")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(mocks.refetch).toHaveBeenCalled();
    expect(mocks.tunnelRefetch).toHaveBeenCalled();
  });

  it("does not call a failed listing offline when the agent is connected", () => {
    mocks.needsAuth = false;
    mocks.isError = true;
    mocks.metadataByTool = { list_devices: storedTool("list_devices", true) };
    renderSection(tunneledProps);

    expect(screen.queryByText(/Tunnel offline/)).toBeNull();
    expect(
      screen.getByText("Couldn't connect to this server to list its tools."),
    ).toBeTruthy();
    // The recorded inventory stays visible.
    expect(screen.getByText("list_devices")).toBeTruthy();
  });

  it("shows a live listing even if an older status read said offline", () => {
    mocks.needsAuth = false;
    mocks.tunnelOffline = true;
    mocks.tools = { list_devices: { annotations: { readOnlyHint: true } } };
    mocks.metadataByTool = { list_devices: storedTool("list_devices", true) };
    renderSection(tunneledProps);

    expect(screen.queryByText(/Tunnel offline/)).toBeNull();
    expect(screen.getByText("list_devices")).toBeTruthy();
  });

  it("offers per-tool actions and no bulk sync", () => {
    mocks.needsAuth = false;
    mocks.tools = { list_devices: { annotations: { readOnlyHint: true } } };
    mocks.metadataByTool = {
      list_devices: storedTool("list_devices", true),
      wipe_device: storedTool("wipe_device"),
    };
    renderSection(tunneledProps);

    expect(mocks.syncArgs.at(-1)?.mode).toBe("additive");
    expect(
      screen.queryByRole("button", { name: /Sync annotations/ }),
    ).toBeNull();
    expect(screen.getByText("not in your listing")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Remove wipe_device/ }));
    expect(mocks.remove).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Remove metadata" }));
    expect(mocks.remove).toHaveBeenCalledWith("wipe_device");
  });

  it("derives nothing from tools kept after a failed refetch", () => {
    mocks.needsAuth = false;
    mocks.isError = true;
    mocks.tools = { list_devices: { annotations: { readOnlyHint: true } } };
    mocks.metadataByTool = { wipe_device: storedTool("wipe_device") };
    renderSection(tunneledProps);

    expect(mocks.syncArgs.at(-1)?.live).toBeUndefined();
    expect(mocks.syncArgs.at(-1)?.enabled).toBe(false);
    expect(screen.queryByText("not in your listing")).toBeNull();
    expect(screen.queryByRole("button", { name: /Remove/ })).toBeNull();
  });

  it("keeps the bulk sync for remote servers", () => {
    mocks.needsAuth = false;
    mocks.tools = { list_devices: { annotations: { readOnlyHint: true } } };
    mocks.metadataByTool = { wipe_device: storedTool("wipe_device") };
    renderSection({ remoteMcpServerId: "remote-1" });

    expect(mocks.syncArgs.at(-1)?.mode).toBe("mirror");
    expect(
      screen.getByRole("button", { name: /Sync annotations/ }),
    ).toBeTruthy();
  });
});
