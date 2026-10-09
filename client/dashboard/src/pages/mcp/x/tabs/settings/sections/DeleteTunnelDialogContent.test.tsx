import { Dialog } from "@/components/ui/Dialog";
import type { SharedTunnelImpactState } from "@/components/mcp/use-shared-tunnel-impact";
import {
  TunnelDeleteIncompleteError,
  TunnelServersChangedError,
} from "@/pages/sources/tunneled-mcp/existingTunnel";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DeleteTunnelDialogContent } from "./DeleteTunnelDialogContent";

const state = vi.hoisted(() => ({
  impact: {} as SharedTunnelImpactState,
  mutateAsync: vi.fn(),
  isError: false,
  error: undefined as Error | undefined,
  toastError: vi.fn(),
  remaining: vi.fn(),
}));

vi.mock("@/components/mcp/use-shared-tunnel-impact", () => ({
  useSharedTunnelImpact: () => state.impact,
}));
vi.mock("@/components/mcp/shared-tunnel-impact", () => ({
  SharedTunnelImpact: ({ impact }: { impact: SharedTunnelImpactState }) => (
    <ul>
      {impact.servers.map((server) => (
        <li key={server.id}>{server.name}</li>
      ))}
    </ul>
  ),
}));
vi.mock("@/pages/sources/tunneled-mcp/hooks", () => ({
  useDeleteTunneledMcpSource: () => ({
    mutateAsync: state.mutateAsync,
    isPending: false,
    isError: state.isError,
    error: state.error,
  }),
}));
vi.mock("./sourceDelete", () => ({
  fetchLinkedMcpServers: state.remaining,
}));
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({}) }));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      add: { tunneled: { href: () => "/mcp/add/tunneled" } },
      x: { settings: { href: (id: string) => `/mcp/x/${id}/settings` } },
    },
  }),
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: state.toastError },
}));

const tunnel = { id: "tunnel-1", name: "JAMF" } as TunneledMcpServer;

function servers(...ids: string[]): McpServer[] {
  return ids.map(
    (id) => ({ id, slug: `slug-${id}`, name: `Server ${id}` }) as McpServer,
  );
}

function setImpact(
  list: McpServer[],
  overrides: Partial<SharedTunnelImpactState> = {},
) {
  state.impact = {
    servers: list,
    isReady: true,
    isLoading: false,
    isError: false,
    retry: state.impact.retry ?? vi.fn<() => void>(),
    ...overrides,
  };
}

const onBusyChange = vi.fn<(busy: boolean) => void>();

function dialog(onLeave: (href?: string) => void) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <Dialog open>
        <Dialog.Content>
          <DeleteTunnelDialogContent
            tunnel={tunnel}
            mcpServerId="a"
            onClose={() => {}}
            onLeave={onLeave}
            onBusyChange={onBusyChange}
          />
        </Dialog.Content>
      </Dialog>
    </QueryClientProvider>
  );
}

function renderDialog() {
  const onLeave = vi.fn<(href?: string) => void>();
  const view = render(dialog(onLeave));
  return { onLeave, rerender: () => view.rerender(dialog(onLeave)) };
}

function input(): HTMLInputElement {
  return screen.getByLabelText(
    "Type the tunnel name to confirm",
  ) as HTMLInputElement;
}

function typeName(value = "JAMF") {
  fireEvent.change(input(), { target: { value } });
}

function deleteButton(): HTMLButtonElement {
  return screen.getByRole("button", { name: "Delete" }) as HTMLButtonElement;
}

async function clickDelete() {
  await act(async () => {
    fireEvent.click(deleteButton());
  });
}

beforeEach(() => {
  state.impact = {} as SharedTunnelImpactState;
  setImpact(servers("a", "b"), { retry: vi.fn<() => void>() });
  state.mutateAsync.mockReset();
  state.toastError.mockReset();
  state.remaining.mockReset();
  state.isError = false;
  state.error = undefined;
  onBusyChange.mockReset();
});

afterEach(cleanup);

describe("DeleteTunnelDialogContent", () => {
  it("deletes exactly the servers the user reviewed", async () => {
    state.mutateAsync.mockResolvedValue(undefined);
    const { onLeave } = renderDialog();
    typeName();
    await clickDelete();
    expect(state.mutateAsync).toHaveBeenCalledWith({
      tunneledMcpServerId: "tunnel-1",
      confirmedMcpServerIds: ["a", "b"],
    });
    expect(onLeave).toHaveBeenCalledWith();
  });

  it("stays disabled until the tunnel's servers are freshly read", () => {
    setImpact(servers("a", "b"), { isReady: false, isLoading: true });
    renderDialog();
    expect(input().disabled).toBe(true);
    expect(deleteButton().disabled).toBe(true);
  });

  it("stays disabled until the tunnel name is typed", () => {
    renderDialog();
    typeName("JAM");
    expect(deleteButton().disabled).toBe(true);
  });

  it.each([
    ["a server was added", servers("a", "b", "c")],
    ["a server was removed", servers("a")],
    ["a server was swapped", servers("a", "c")],
  ])(
    "disarms a typed confirmation when %s in a background refresh",
    async (_case, refreshed) => {
      state.mutateAsync.mockResolvedValue(undefined);
      const { rerender } = renderDialog();
      typeName();
      expect(deleteButton().disabled).toBe(false);

      setImpact(refreshed);
      rerender();

      expect(deleteButton().disabled).toBe(true);
      expect(input().value).toBe("");
      expect(screen.getByText(/changed after you confirmed/)).toBeTruthy();

      typeName();
      await clickDelete();
      expect(state.mutateAsync).toHaveBeenCalledWith({
        tunneledMcpServerId: "tunnel-1",
        confirmedMcpServerIds: refreshed.map((server) => server.id),
      });
    },
  );

  it("keeps a confirmation when only the order of the list changes", () => {
    const { rerender } = renderDialog();
    typeName();
    setImpact(servers("b", "a"));
    rerender();
    expect(deleteButton().disabled).toBe(false);
  });

  it("asks again when the servers changed at the last moment", async () => {
    state.mutateAsync.mockRejectedValue(new TunnelServersChangedError());
    const { onLeave } = renderDialog();
    typeName();
    await clickDelete();
    expect(state.impact.retry).toHaveBeenCalledOnce();
    expect(input().value).toBe("");
    expect(onLeave).not.toHaveBeenCalled();
  });

  it("stays to show what is left when this server survived a partial delete", async () => {
    state.mutateAsync.mockRejectedValue(
      new TunnelDeleteIncompleteError("Deleted 1 of 2 MCP servers.", true),
    );
    state.remaining.mockResolvedValue(servers("a"));
    const { onLeave } = renderDialog();
    typeName();
    await clickDelete();
    expect(onLeave).not.toHaveBeenCalled();
    expect(state.impact.retry).toHaveBeenCalledOnce();
    expect(input().value).toBe("");
  });

  it("continues from a surviving server once this one is gone", async () => {
    state.mutateAsync.mockRejectedValue(
      new TunnelDeleteIncompleteError("Deleted 1 of 2 MCP servers.", true),
    );
    state.remaining.mockResolvedValue(servers("b"));
    const { onLeave } = renderDialog();
    typeName();
    await clickDelete();
    expect(onLeave).toHaveBeenCalledWith("/mcp/x/slug-b/settings");
    expect(state.toastError).toHaveBeenCalledWith(
      expect.stringContaining("Server b still uses the tunnel"),
      expect.anything(),
    );
  });

  it("finishes from the tunnel list when no visible server is left", async () => {
    state.mutateAsync.mockRejectedValue(
      new TunnelDeleteIncompleteError(
        "Deleted 2 MCP servers, but the tunnel could not be confirmed deleted.",
        true,
      ),
    );
    state.remaining.mockResolvedValue([]);
    const { onLeave } = renderDialog();
    typeName();
    await clickDelete();
    expect(onLeave).toHaveBeenCalledWith("/mcp/add/tunneled?tunnel=tunnel-1");
    expect(state.toastError).toHaveBeenCalledWith(
      expect.stringContaining("ask a project admin"),
      expect.anything(),
    );
  });

  it("reports what is left as unknown when it cannot be read", async () => {
    state.mutateAsync.mockRejectedValue(
      new TunnelDeleteIncompleteError("Deleted 1 of 2 MCP servers.", true),
    );
    state.remaining.mockRejectedValue(new Error("offline"));
    const { onLeave } = renderDialog();
    typeName();
    await clickDelete();
    // The tunnel list reads the servers again and links each survivor.
    expect(onLeave).toHaveBeenCalledWith("/mcp/add/tunneled?tunnel=tunnel-1");
    const message = state.toastError.mock.calls[0]?.[0] as string;
    expect(message).toContain("Could not check which MCP servers still use");
    expect(message).not.toContain("No MCP server you can view");
  });

  it("stays busy until recovery from a partial delete settles", async () => {
    state.mutateAsync.mockRejectedValue(
      new TunnelDeleteIncompleteError("Deleted 1 of 2 MCP servers.", true),
    );
    let finishRecovery: (servers: McpServer[]) => void = () => {};
    state.remaining.mockReturnValue(
      new Promise<McpServer[]>((done) => {
        finishRecovery = done;
      }),
    );
    const { onLeave } = renderDialog();
    typeName();
    await clickDelete();

    // The mutation has rejected, but recovery is still reading what is left.
    expect(onBusyChange).toHaveBeenLastCalledWith(true);
    expect(
      (screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);

    await act(async () => finishRecovery(servers("b")));
    expect(onLeave).toHaveBeenCalled();
    expect(onBusyChange).toHaveBeenLastCalledWith(false);
  });

  it("stays open for a retry when nothing was deleted", async () => {
    const failure = new TunnelDeleteIncompleteError(
      "Deleted 0 MCP servers, but the tunnel could not be confirmed deleted.",
      false,
    );
    state.mutateAsync.mockRejectedValue(failure);
    const { onLeave, rerender } = renderDialog();
    typeName();
    await clickDelete();
    expect(onLeave).not.toHaveBeenCalled();
    expect(state.remaining).not.toHaveBeenCalled();

    // The mutation now reports the failure, which the dialog shows.
    state.isError = true;
    state.error = failure;
    rerender();
    expect(screen.getByText(failure.message)).toBeTruthy();
  });
});
