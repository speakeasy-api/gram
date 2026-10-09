import { TooltipProvider } from "@/components/ui/Tooltip";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  renderHook,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TunnelKeySection } from "./TunnelKeySection";
import { UpstreamUrlField } from "./UpstreamUrlField";
import type { UpstreamUrlDraft } from "./useUpstreamUrlDraft";
import {
  SOURCE_DESTINATION_LOCK_REASON,
  SOURCE_DESTINATION_UNKNOWN_REASON,
  useSourceDestinationLock,
} from "./useSourceDestinationLock";

const PROJECT = "project-1";

type Sibling = {
  id: string;
  environmentId?: string;
  remoteMcpServerId?: string;
  tunneledMcpServerId?: string;
  visibility: string;
};

const mocks = vi.hoisted(() => ({
  // Whether the caller holds project-wide environment:read. mcp:write is
  // always held, so only the environment rule decides.
  canReadEnvironments: false,
  rbacLoading: false,
  siblings: {
    data: undefined as { mcpServers: Sibling[] } | undefined,
    isLoading: false,
    isError: false,
  },
  listArgs: vi.fn(),
}));

vi.mock("@/hooks/useRBAC", () => {
  const has = (scope: string, resourceId?: string, projectId?: string) => {
    if (scope === "mcp:write") return true;
    // The lock must ask for the project-wide grant, not one environment.
    return (
      scope === "environment:read" &&
      resourceId === PROJECT &&
      projectId === PROJECT &&
      mocks.canReadEnvironments
    );
  };
  return {
    useRBAC: () => ({
      hasScope: has,
      hasAnyScope: (
        scopes: string[],
        resourceId?: string,
        projectId?: string,
      ) => scopes.some((scope) => has(scope, resourceId, projectId)),
      hasAllScopes: (
        scopes: string[],
        resourceId?: string,
        projectId?: string,
      ) => scopes.every((scope) => has(scope, resourceId, projectId)),
      isLoading: mocks.rbacLoading,
    }),
  };
});

vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: (request: unknown, _security: unknown, options: unknown) => {
    mocks.listArgs(request, options);
    return mocks.siblings;
  },
}));

vi.mock("@/pages/sources/tunneled-mcp/hooks", () => ({
  useRotateTunneledMcpServerKey: () => ({
    mutateAsync: vi.fn(),
    reset: vi.fn(),
    isPending: false,
  }),
}));

function siblings(list: Sibling[]) {
  mocks.siblings = {
    data: { mcpServers: list },
    isLoading: false,
    isError: false,
  };
}

beforeEach(() => {
  mocks.canReadEnvironments = false;
  mocks.rbacLoading = false;
  siblings([]);
  mocks.listArgs.mockReset();
});

afterEach(cleanup);

const tunneled = {
  kind: "tunneled",
  id: "tunnel-1",
  projectId: PROJECT,
} as const;
const remote = { kind: "remote", id: "remote-1", projectId: PROJECT } as const;

describe("useSourceDestinationLock", () => {
  it("locks a source when a disabled sibling has a linked environment", () => {
    siblings([
      { id: "a", tunneledMcpServerId: "tunnel-1", visibility: "private" },
      {
        id: "b",
        tunneledMcpServerId: "tunnel-1",
        environmentId: "env-1",
        visibility: "disabled",
      },
    ]);
    const { result } = renderHook(() => useSourceDestinationLock(tunneled));
    expect(result.current).toEqual({
      locked: true,
      reason: SOURCE_DESTINATION_LOCK_REASON,
    });
    expect(mocks.listArgs).toHaveBeenCalledWith(
      { tunneledMcpServerId: "tunnel-1" },
      expect.objectContaining({ enabled: true }),
    );
  });

  it("filters remote sources by their own id", () => {
    siblings([
      {
        id: "a",
        remoteMcpServerId: "remote-1",
        environmentId: "env-1",
        visibility: "private",
      },
    ]);
    const { result } = renderHook(() => useSourceDestinationLock(remote));
    expect(result.current.locked).toBe(true);
    expect(mocks.listArgs).toHaveBeenCalledWith(
      { remoteMcpServerId: "remote-1" },
      expect.anything(),
    );
  });

  it("leaves a source with no linked server unlocked", () => {
    siblings([
      { id: "a", tunneledMcpServerId: "tunnel-1", visibility: "private" },
      // A linked server on another source does not count.
      {
        id: "b",
        tunneledMcpServerId: "tunnel-2",
        environmentId: "env-1",
        visibility: "private",
      },
    ]);
    const { result } = renderHook(() => useSourceDestinationLock(tunneled));
    expect(result.current).toEqual({ locked: false, reason: null });
  });

  it("unlocks a caller with project-wide environment:read without listing", () => {
    mocks.canReadEnvironments = true;
    siblings([
      {
        id: "a",
        tunneledMcpServerId: "tunnel-1",
        environmentId: "env-1",
        visibility: "private",
      },
    ]);
    const { result } = renderHook(() => useSourceDestinationLock(tunneled));
    expect(result.current).toEqual({ locked: false, reason: null });
    expect(mocks.listArgs).toHaveBeenCalledWith(
      expect.anything(),
      expect.objectContaining({ enabled: false }),
    );
  });

  it("stays locked while the sibling list is loading or failed", () => {
    mocks.siblings = { data: undefined, isLoading: true, isError: false };
    expect(
      renderHook(() => useSourceDestinationLock(tunneled)).result.current,
    ).toEqual({ locked: true, reason: SOURCE_DESTINATION_UNKNOWN_REASON });
    mocks.siblings = { data: undefined, isLoading: false, isError: true };
    expect(
      renderHook(() => useSourceDestinationLock(tunneled)).result.current
        .locked,
    ).toBe(true);
  });
});

function renderWithProviders(ui: React.ReactElement) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>{ui}</TooltipProvider>
    </QueryClientProvider>,
  );
}

const tunnel = {
  id: "tunnel-1",
  projectId: PROJECT,
  name: "jamf",
  keyPrefix: "gram_tun_",
} as unknown as TunneledMcpServer;

describe("TunnelKeySection", () => {
  it("blocks rotation while a linked server needs environment authority", () => {
    siblings([
      {
        id: "a",
        tunneledMcpServerId: "tunnel-1",
        environmentId: "env-1",
        visibility: "private",
      },
    ]);
    renderWithProviders(<TunnelKeySection tunneledMcpServer={tunnel} />);
    fireEvent.click(screen.getByRole("button", { name: /rotate key/i }));
    expect(screen.queryByText("Rotate Tunnel Key")).toBeNull();
  });

  it("allows rotation for a caller with environment authority", () => {
    mocks.canReadEnvironments = true;
    siblings([
      {
        id: "a",
        tunneledMcpServerId: "tunnel-1",
        environmentId: "env-1",
        visibility: "private",
      },
    ]);
    renderWithProviders(<TunnelKeySection tunneledMcpServer={tunnel} />);
    fireEvent.click(screen.getByRole("button", { name: /rotate key/i }));
    expect(screen.getByText("Rotate Tunnel Key")).toBeTruthy();
  });

  it("allows rotation when no server on the tunnel is linked", () => {
    siblings([
      { id: "a", tunneledMcpServerId: "tunnel-1", visibility: "private" },
    ]);
    renderWithProviders(<TunnelKeySection tunneledMcpServer={tunnel} />);
    fireEvent.click(screen.getByRole("button", { name: /rotate key/i }));
    expect(screen.getByText("Rotate Tunnel Key")).toBeTruthy();
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

  it("keeps the URL editable when unlocked", () => {
    renderWithProviders(<UpstreamUrlField upstream={draft(null)} />);
    expect(
      (screen.getByLabelText("Remote URL") as HTMLInputElement).disabled,
    ).toBe(false);
    expect(screen.queryByText(SOURCE_DESTINATION_LOCK_REASON)).toBeNull();
  });
});
