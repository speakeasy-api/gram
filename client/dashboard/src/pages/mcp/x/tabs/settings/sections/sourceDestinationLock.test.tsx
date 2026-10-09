import { TooltipProvider } from "@/components/ui/Tooltip";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  SOURCE_DESTINATION_LOCK_REASON,
  SOURCE_DESTINATION_UNKNOWN_REASON,
  sourceDestinationLock,
  withUnconfirmedEnvironmentLink,
} from "./sourceDestinationLock";
import { TunnelKeySection } from "./TunnelKeySection";
import { UpstreamUrlField } from "./UpstreamUrlField";
import {
  type UpstreamUrlDraft,
  useUpstreamUrlDraft,
} from "./useUpstreamUrlDraft";

const PROJECT = "project-1";

const mocks = vi.hoisted(() => ({
  // The caller holds every scope the dashboard can see, including a broad
  // environment:read, unless canWrite is cleared. Only the server's answer
  // may lock a source move, so a local grant must never unlock one.
  canWrite: true,
  rotate: vi.fn(),
  updateRemote: vi.fn(),
  invalidateTunneled: vi.fn(async () => {}),
  invalidateRemote: vi.fn(async () => {}),
}));

vi.mock("@/hooks/useRBAC", () => {
  const has = (scope: string) => scope !== "mcp:write" || mocks.canWrite;
  return {
    useRBAC: () => ({
      hasScope: has,
      hasAnyScope: (scopes: string[]) => scopes.some(has),
      hasAllScopes: (scopes: string[]) => scopes.every(has),
      isLoading: false,
    }),
  };
});

vi.mock("@/pages/sources/tunneled-mcp/hooks", () => ({
  useRotateTunneledMcpServerKey: () => ({
    mutateAsync: mocks.rotate,
    reset: vi.fn(() => {}),
    isPending: false,
  }),
}));

vi.mock("@gram/client/react-query/updateRemoteMcpServer.js", () => ({
  useUpdateRemoteMcpServerMutation: () => ({
    mutateAsync: mocks.updateRemote,
    isPending: false,
    error: null,
  }),
}));

vi.mock("@/pages/sources/remote-mcp/useVerifyRemoteMcpUrl", () => ({
  useVerifyRemoteMcpUrl: () => ({ status: "idle" }),
}));

vi.mock("./sourceInvalidation", () => ({
  invalidateTunneledMcpSourceViews: mocks.invalidateTunneled,
  invalidateRemoteMcpSourceViews: mocks.invalidateRemote,
}));

function forbidden(): GramError {
  const error = Object.create(GramError.prototype) as GramError;
  Object.assign(error, { statusCode: 403, message: "forbidden" });
  return error;
}

beforeEach(() => {
  mocks.canWrite = true;
  mocks.rotate.mockReset();
  mocks.updateRemote.mockReset();
  mocks.invalidateTunneled.mockClear();
  mocks.invalidateRemote.mockClear();
});

afterEach(cleanup);

describe("sourceDestinationLock", () => {
  it("follows the server's answer for this caller", () => {
    expect(sourceDestinationLock({ environmentLinkAuthorized: true })).toEqual({
      reason: null,
    });
    expect(sourceDestinationLock({ environmentLinkAuthorized: false })).toEqual(
      { reason: SOURCE_DESTINATION_LOCK_REASON },
    );
    expect(sourceDestinationLock({})).toEqual({
      reason: SOURCE_DESTINATION_UNKNOWN_REASON,
    });
  });

  it("forgets the answer from a failed refresh but keeps the row", () => {
    const row = {
      id: "s",
      environmentLinked: false,
      environmentLinkAuthorized: true,
    };
    expect(withUnconfirmedEnvironmentLink(row, false)).toBe(row);
    expect(withUnconfirmedEnvironmentLink(row, true)).toEqual({
      id: "s",
      environmentLinked: undefined,
      environmentLinkAuthorized: undefined,
    });
  });
});

function tunnel(environmentLinkAuthorized: boolean | undefined) {
  return {
    id: "tunnel-1",
    projectId: PROJECT,
    name: "jamf",
    keyPrefix: "gram_tun_",
    environmentLinkAuthorized,
  } as unknown as TunneledMcpServer;
}

function tunnelSection(authorized: boolean | undefined) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <TunnelKeySection tunneledMcpServer={tunnel(authorized)} />
      </TooltipProvider>
    </QueryClientProvider>
  );
}

function rotateTrigger(): HTMLButtonElement {
  return screen.getByRole("button", {
    name: /rotate key/i,
  }) as HTMLButtonElement;
}

function confirmButton(): HTMLButtonElement {
  return screen.getByRole("button", { name: /^rotate$/i }) as HTMLButtonElement;
}

describe("TunnelKeySection", () => {
  it.each([
    ["refused", false],
    ["unconfirmed", undefined],
  ])(
    "disables rotation when the server's answer is %s, whatever the local grants",
    (_, authorized) => {
      render(tunnelSection(authorized));
      expect(rotateTrigger().disabled).toBe(true);
      fireEvent.click(rotateTrigger());
      expect(screen.queryByText("Rotate Tunnel Key")).toBeNull();
    },
  );

  it("allows rotation when the server says the caller may", () => {
    render(tunnelSection(true));
    fireEvent.click(rotateTrigger());
    expect(screen.getByText("Rotate Tunnel Key")).toBeTruthy();
  });

  it("disables an open confirmation once the source becomes locked", () => {
    const view = render(tunnelSection(true));
    fireEvent.click(rotateTrigger());
    expect(confirmButton().disabled).toBe(false);

    for (const [authorized, reason] of [
      [false, SOURCE_DESTINATION_LOCK_REASON],
      [undefined, SOURCE_DESTINATION_UNKNOWN_REASON],
    ] as const) {
      view.rerender(tunnelSection(authorized));
      expect(confirmButton().disabled).toBe(true);
      expect(screen.getByText(reason)).toBeTruthy();
      fireEvent.click(confirmButton());
      expect(mocks.rotate).not.toHaveBeenCalled();
    }

    view.rerender(tunnelSection(true));
    expect(confirmButton().disabled).toBe(false);
  });

  it("disables an open confirmation once mcp:write is lost", () => {
    const view = render(tunnelSection(true));
    fireEvent.click(rotateTrigger());
    mocks.canWrite = false;
    view.rerender(tunnelSection(true));
    expect(confirmButton().disabled).toBe(true);
    fireEvent.click(confirmButton());
    expect(mocks.rotate).not.toHaveBeenCalled();
  });

  it("refreshes the source when the server refuses a rotation the cache allowed", async () => {
    mocks.rotate.mockRejectedValue(forbidden());
    render(tunnelSection(true));
    fireEvent.click(rotateTrigger());
    fireEvent.click(confirmButton());
    await waitFor(() => expect(mocks.invalidateTunneled).toHaveBeenCalled());
  });
});

function draft(lockedReason: string | null): UpstreamUrlDraft {
  return {
    draft: "https://example.com/mcp",
    setDraft: vi.fn(() => {}),
    touch: vi.fn(() => {}),
    fieldError: null,
    dirty: false,
    invalid: false,
    pending: false,
    lockedReason,
    verify: { status: "idle" } as unknown as UpstreamUrlDraft["verify"],
    save: vi.fn(async () => {}),
  };
}

function renderWithProviders(ui: React.ReactElement) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>{ui}</TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("UpstreamUrlField", () => {
  it("disables the URL and says why when the destination is locked", () => {
    renderWithProviders(
      <UpstreamUrlField upstream={draft(SOURCE_DESTINATION_LOCK_REASON)} />,
    );
    expect(
      (screen.getByLabelText("Remote URL") as HTMLInputElement).disabled,
    ).toBe(true);
    expect(screen.getByText(SOURCE_DESTINATION_LOCK_REASON)).toBeTruthy();
  });

  it("describes the input by both the error and the lock reason", () => {
    renderWithProviders(
      <UpstreamUrlField
        upstream={{
          ...draft(SOURCE_DESTINATION_LOCK_REASON),
          fieldError: "Enter a valid URL",
        }}
      />,
    );
    expect(
      screen.getByLabelText("Remote URL").getAttribute("aria-describedby"),
    ).toBe("mcp-upstream-url-error mcp-upstream-url-locked");
  });

  it("keeps the URL editable when unlocked", () => {
    renderWithProviders(<UpstreamUrlField upstream={draft(null)} />);
    expect(
      (screen.getByLabelText("Remote URL") as HTMLInputElement).disabled,
    ).toBe(false);
  });
});

describe("useUpstreamUrlDraft", () => {
  const remote = {
    id: "remote-1",
    projectId: PROJECT,
    url: "https://example.com/mcp",
    environmentLinkAuthorized: true,
  } as unknown as RemoteMcpServer;

  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={new QueryClient()}>
      {children}
    </QueryClientProvider>
  );

  it("refreshes the source and rethrows when the server refuses the URL", async () => {
    mocks.updateRemote.mockRejectedValue(forbidden());
    const { result } = renderHook(() => useUpstreamUrlDraft(remote), {
      wrapper,
    });
    act(() => result.current.setDraft("https://example.com/moved"));
    await expect(result.current.save()).rejects.toBeInstanceOf(GramError);
    expect(mocks.invalidateRemote).toHaveBeenCalled();
    expect(mocks.updateRemote).toHaveBeenCalledTimes(1);
  });

  it("shows the canonical URL, not an unsavable draft, while the source is locked", () => {
    const { result, rerender } = renderHook(
      ({ source }: { source: RemoteMcpServer }) => useUpstreamUrlDraft(source),
      { wrapper, initialProps: { source: remote } },
    );
    act(() => result.current.setDraft("https://example.com/moved"));
    expect(result.current.dirty).toBe(true);

    rerender({ source: { ...remote, environmentLinkAuthorized: false } });
    expect(result.current.lockedReason).toBe(SOURCE_DESTINATION_LOCK_REASON);
    expect(result.current.draft).toBe(remote.url);
    expect(result.current.dirty).toBe(false);
    expect(result.current.fieldError).toBeFalsy();

    // A lock that clears (e.g. a failed refresh that later succeeds) brings
    // the typed URL back.
    rerender({ source: remote });
    expect(result.current.draft).toBe("https://example.com/moved");
    expect(result.current.dirty).toBe(true);
  });
});
