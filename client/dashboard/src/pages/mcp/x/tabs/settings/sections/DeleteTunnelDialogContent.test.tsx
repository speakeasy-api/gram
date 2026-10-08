import { Dialog } from "@/components/ui/Dialog";
import type { SharedTunnelImpactState } from "@/components/mcp/use-shared-tunnel-impact";
import {
  TunnelDeleteIncompleteError,
  TunnelServersChangedError,
} from "@/pages/sources/tunneled-mcp/existingTunnel";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
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
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: state.toastError },
}));

const tunnel = { id: "tunnel-1", name: "JAMF" } as TunneledMcpServer;

function servers(...ids: string[]): McpServer[] {
  return ids.map((id) => ({ id, name: `Server ${id}` }) as McpServer);
}

function renderDialog() {
  const onClose = vi.fn<() => void>();
  const onLeave = vi.fn<() => void>();
  render(
    <Dialog open>
      <Dialog.Content>
        <DeleteTunnelDialogContent
          tunnel={tunnel}
          mcpServerId="a"
          onClose={onClose}
          onLeave={onLeave}
        />
      </Dialog.Content>
    </Dialog>,
  );
  return { onClose, onLeave };
}

function typeName(value = "JAMF") {
  fireEvent.change(screen.getByLabelText("Type the tunnel name to confirm"), {
    target: { value },
  });
}

function deleteButton(): HTMLButtonElement {
  return screen.getByRole("button", { name: "Delete" }) as HTMLButtonElement;
}

beforeEach(() => {
  state.impact = {
    servers: servers("a", "b"),
    isReady: true,
    isLoading: false,
    isError: false,
    retry: vi.fn<() => void>(),
  };
  state.mutateAsync.mockReset();
  state.toastError.mockReset();
  state.isError = false;
  state.error = undefined;
});

afterEach(cleanup);

describe("DeleteTunnelDialogContent", () => {
  it("deletes exactly the servers the user reviewed", async () => {
    state.mutateAsync.mockResolvedValue(undefined);
    const { onLeave } = renderDialog();
    typeName();
    await act(async () => {
      fireEvent.click(deleteButton());
    });
    expect(state.mutateAsync).toHaveBeenCalledWith({
      tunneledMcpServerId: "tunnel-1",
      confirmedMcpServerIds: ["a", "b"],
    });
    expect(onLeave).toHaveBeenCalledOnce();
  });

  it("stays disabled until the tunnel's servers are freshly read", () => {
    state.impact = { ...state.impact, isReady: false, isLoading: true };
    renderDialog();
    typeName();
    expect(deleteButton().disabled).toBe(true);
  });

  it("stays disabled until the tunnel name is typed", () => {
    renderDialog();
    typeName("JAM");
    expect(deleteButton().disabled).toBe(true);
  });

  it("asks again when the servers changed, without leaving", async () => {
    state.mutateAsync.mockRejectedValue(new TunnelServersChangedError());
    const { onLeave } = renderDialog();
    typeName();
    await act(async () => {
      fireEvent.click(deleteButton());
    });
    expect(state.impact.retry).toHaveBeenCalledOnce();
    expect(
      (
        screen.getByLabelText(
          "Type the tunnel name to confirm",
        ) as HTMLInputElement
      ).value,
    ).toBe("");
    expect(onLeave).not.toHaveBeenCalled();
  });

  it("leaves the page with the outcome once servers may be gone", async () => {
    state.mutateAsync.mockRejectedValue(
      new TunnelDeleteIncompleteError("Deleted 1 of 2", true),
    );
    const { onLeave } = renderDialog();
    typeName();
    await act(async () => {
      fireEvent.click(deleteButton());
    });
    expect(state.toastError).toHaveBeenCalledWith(
      "Deleted 1 of 2",
      expect.anything(),
    );
    expect(onLeave).toHaveBeenCalledOnce();
  });

  it("stays open for a retry when nothing was deleted", async () => {
    state.mutateAsync.mockRejectedValue(
      new TunnelDeleteIncompleteError("Retry to finish", false),
    );
    const { onLeave } = renderDialog();
    typeName();
    await act(async () => {
      fireEvent.click(deleteButton());
    });
    expect(onLeave).not.toHaveBeenCalled();
  });
});
