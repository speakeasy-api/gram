import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RevokeSessionsDialog } from "./RevokeSessionsDialog";

const state = vi.hoisted(() => ({
  revoke: vi.fn(),
  createKillswitch: vi.fn(),
}));

vi.mock("@gram/client/react-query/revokeUserSession.js", () => ({
  useRevokeUserSessionMutation: () => ({ mutateAsync: state.revoke }),
}));
vi.mock("@gram/client/react-query/createKillswitch.js", () => ({
  useCreateKillswitchMutation: () => ({ mutateAsync: state.createKillswitch }),
}));

beforeEach(() => {
  state.revoke.mockReset().mockResolvedValue(undefined);
  state.createKillswitch.mockReset();
});
afterEach(cleanup);

describe("RevokeSessionsDialog", () => {
  it("cross-links an exact user while keeping revocation independent", async () => {
    const user = userEvent.setup();
    const onRevoked = vi.fn();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>
          <RevokeSessionsDialog
            sessionIds={["session-1", "session-2"]}
            newKillswitchHref="/example/killswitch?create=1&createUser=user-1&createCapability=mcp_tool_calls"
            open
            onOpenChange={() => {}}
            onRevoked={(ids) => {
              onRevoked(ids);
            }}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );

    expect(
      screen.getByText(/clients can authenticate and reconnect/i),
    ).not.toBeNull();
    expect(
      screen.getByText(/Revocation never creates or lifts/i),
    ).not.toBeNull();
    const link = screen.getByRole("link", { name: "New killswitch…" });
    expect(link.getAttribute("href")).toContain("createUser=user-1");
    await user.click(link);
    expect(state.revoke).not.toHaveBeenCalled();
    expect(state.createKillswitch).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Revoke 2" }));
    await waitFor(() => expect(state.revoke).toHaveBeenCalledTimes(2));
    expect(state.createKillswitch).not.toHaveBeenCalled();
    expect(onRevoked).toHaveBeenCalledWith(["session-1", "session-2"]);
  });

  it("does not show a Killswitch action without one exact user context", () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>
          <RevokeSessionsDialog
            sessionIds={["session-1", "session-2"]}
            open
            onOpenChange={() => {}}
            onRevoked={() => {}}
          />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(screen.queryByRole("link", { name: "New killswitch…" })).toBeNull();
  });
});
