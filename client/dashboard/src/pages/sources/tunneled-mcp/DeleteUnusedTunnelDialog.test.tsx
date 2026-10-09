import { ServiceError } from "@gram/client/models/errors/serviceerror.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import {
  QueryClient,
  QueryClientProvider,
  useMutation,
} from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DeleteUnusedTunnelDialog } from "./DeleteUnusedTunnelDialog";

const state = vi.hoisted(() => ({
  deleteTunnel:
    vi.fn<(vars: { tunneledMcpServerId: string }) => Promise<void>>(),
}));

// A real mutation over a mocked request, so pending and error states are the
// ones TanStack Query produces.
vi.mock("./hooks", () => ({
  useDeleteUnusedTunnel: () => useMutation({ mutationFn: state.deleteTunnel }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const tunnel = { id: "tunnel-1", name: "JAMF" } as TunneledMcpServer;

function conflict(): ServiceError {
  const body = JSON.stringify({ message: "in use" });
  return new ServiceError(
    {
      name: "conflict",
      message: "in use",
      id: "err",
      fault: false,
      temporary: false,
      timeout: false,
    },
    {
      response: new Response(body, { status: 409 }),
      request: new Request("http://localhost/rpc"),
      body,
    },
  );
}

function renderDialog() {
  const onClose = vi.fn<() => void>();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <DeleteUnusedTunnelDialog tunnel={tunnel} onClose={onClose} />
    </QueryClientProvider>,
  );
  return onClose;
}

function deleteButton(): HTMLButtonElement {
  return screen.getByRole("button", { name: /^Delet/ }) as HTMLButtonElement;
}

function typeName(value = "JAMF") {
  fireEvent.change(screen.getByLabelText("Type the tunnel name to confirm"), {
    target: { value },
  });
}

beforeEach(() => {
  state.deleteTunnel.mockReset();
});

afterEach(cleanup);

describe("DeleteUnusedTunnelDialog", () => {
  it("deletes only after the tunnel name is typed, then closes", async () => {
    state.deleteTunnel.mockResolvedValue(undefined);
    const onClose = renderDialog();
    expect(deleteButton().disabled).toBe(true);
    typeName("JAM");
    expect(deleteButton().disabled).toBe(true);
    typeName();

    await act(async () => {
      fireEvent.click(deleteButton());
    });
    expect(state.deleteTunnel).toHaveBeenCalledOnce();
    expect(state.deleteTunnel.mock.calls[0]?.[0]).toEqual({
      tunneledMcpServerId: "tunnel-1",
    });
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("explains a refusal because the tunnel is still in use", async () => {
    state.deleteTunnel.mockRejectedValue(conflict());
    const onClose = renderDialog();
    typeName();
    await act(async () => {
      fireEvent.click(deleteButton());
    });
    expect(
      await screen.findByText(
        /still in use, possibly by MCP servers you cannot view/,
      ),
    ).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("cannot be closed while the delete is in flight", async () => {
    let finish: () => void = () => {};
    state.deleteTunnel.mockReturnValue(
      new Promise<void>((done) => {
        finish = done;
      }),
    );
    const onClose = renderDialog();
    typeName();
    await act(async () => {
      fireEvent.click(deleteButton());
    });

    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Close" })).toBeNull(),
    );
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    expect(onClose).not.toHaveBeenCalled();

    await act(async () => finish());
    await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
  });
});
