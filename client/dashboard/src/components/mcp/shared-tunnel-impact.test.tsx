import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SharedTunnelConfirmDialog } from "./shared-tunnel-impact";
import { PUBLIC_SIBLING_WARNING } from "./use-shared-tunnel-impact";

type QueryState = {
  servers: McpServer[];
  isSuccess: boolean;
  isError: boolean;
  isFetching: boolean;
  dataUpdatedAt: number;
};

const query = vi.hoisted(() => ({
  state: {} as QueryState,
  refetch: vi.fn(),
  lastOptions: undefined as unknown,
}));

vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: (_request: unknown, _security: unknown, options: unknown) => {
    query.lastOptions = options;
    return {
      data: { mcpServers: query.state.servers },
      isSuccess: query.state.isSuccess,
      isError: query.state.isError,
      isFetching: query.state.isFetching,
      dataUpdatedAt: query.state.dataUpdatedAt,
      refetch: query.refetch,
    };
  },
}));

const FRESH = Number.MAX_SAFE_INTEGER;

function server(
  id: string,
  visibility: McpServer["visibility"] = "private",
): McpServer {
  return {
    id,
    name: `Server ${id}`,
    visibility,
    tunneledMcpServerId: "tunnel-1",
  } as McpServer;
}

function renderDialog(onConfirm = vi.fn<() => void>()) {
  render(
    <SharedTunnelConfirmDialog
      open
      onOpenChange={() => {}}
      tunneledMcpServerId="tunnel-1"
      tunnelName="JAMF"
      currentMcpServerId="a"
      title="Change it?"
      description="Tunnel-wide change."
      confirmLabel="Save"
      pendingLabel="Saving"
      isPending={false}
      onConfirm={onConfirm}
    />,
  );
  return onConfirm;
}

function confirmButton(): HTMLButtonElement {
  return screen.getByRole("button", { name: "Save" }) as HTMLButtonElement;
}

beforeEach(() => {
  query.refetch.mockReset();
  query.state = {
    servers: [server("a"), server("b")],
    isSuccess: true,
    isError: false,
    isFetching: false,
    dataUpdatedAt: FRESH,
  };
});

afterEach(cleanup);

describe("SharedTunnelConfirmDialog", () => {
  it("lists the servers on the tunnel and enables Confirm after a fresh read", () => {
    const onConfirm = renderDialog();
    expect(screen.getByText("Server a")).toBeTruthy();
    expect(screen.getByText("Server b")).toBeTruthy();
    expect(screen.getByText("(this server)")).toBeTruthy();
    expect(screen.getByText("MCP servers you can view (2)")).toBeTruthy();
    expect(screen.getByText(/including any you cannot view/)).toBeTruthy();
    expect(query.refetch).toHaveBeenCalled();
    expect(query.lastOptions).toMatchObject({
      staleTime: 0,
      throwOnError: false,
    });

    fireEvent.click(confirmButton());
    expect(onConfirm).toHaveBeenCalledOnce();
  });

  it("keeps Confirm disabled while only a cached list is available", () => {
    query.state.dataUpdatedAt = 1;
    renderDialog();
    expect(confirmButton().disabled).toBe(true);
  });

  it("keeps Confirm disabled while the list is refetching", () => {
    query.state.isFetching = true;
    renderDialog();
    expect(confirmButton().disabled).toBe(true);
  });

  it("offers a retry and keeps Confirm disabled when the read fails", () => {
    query.state = { ...query.state, isSuccess: false, isError: true };
    renderDialog();
    expect(confirmButton().disabled).toBe(true);
    query.refetch.mockClear();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(query.refetch).toHaveBeenCalledOnce();
  });

  it("warns that a public sibling bypasses private access control", () => {
    query.state.servers = [server("a"), server("b", "public")];
    renderDialog();
    expect(screen.getByText(PUBLIC_SIBLING_WARNING)).toBeTruthy();
  });

  it("omits the public warning when every visible server is private", () => {
    renderDialog();
    expect(screen.queryByText(PUBLIC_SIBLING_WARNING)).toBeNull();
  });

  it("ignores servers on other tunnels in a stale or unfiltered response", () => {
    query.state.servers = [
      server("a"),
      { ...server("x"), tunneledMcpServerId: "tunnel-2" },
    ];
    renderDialog();
    expect(screen.queryByText("Server x")).toBeNull();
    expect(screen.getByText("MCP servers you can view (1)")).toBeTruthy();
  });
});
