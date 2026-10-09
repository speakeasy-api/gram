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

const mocks = vi.hoisted(() => ({
  // Whether the caller holds project-wide environment:read. mcp:write is held
  // unless canWrite is cleared, so only the environment rule decides.
  canReadEnvironments: false,
  canWrite: true,
  rbacLoading: false,
  rotate: vi.fn(),
}));

vi.mock("@/hooks/useRBAC", () => {
  const has = (scope: string, resourceId?: string, projectId?: string) => {
    if (scope === "mcp:write") return mocks.canWrite;
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

vi.mock("@/pages/sources/tunneled-mcp/hooks", () => ({
  useRotateTunneledMcpServerKey: () => ({
    mutateAsync: mocks.rotate,
    reset: vi.fn(() => {}),
    isPending: false,
  }),
}));

beforeEach(() => {
  mocks.canReadEnvironments = false;
  mocks.canWrite = true;
  mocks.rbacLoading = false;
  mocks.rotate.mockReset();
});

afterEach(cleanup);

function lockFor(environmentLinked: boolean | undefined) {
  return renderHook(() =>
    useSourceDestinationLock({ projectId: PROJECT, environmentLinked }),
  ).result.current;
}

describe("useSourceDestinationLock", () => {
  it("locks a linked source for a caller without project-wide environment:read", () => {
    expect(lockFor(true)).toEqual({ reason: SOURCE_DESTINATION_LOCK_REASON });
  });

  it("leaves an unlinked source unlocked", () => {
    expect(lockFor(false)).toEqual({ reason: null });
  });

  it("unlocks a linked source for a caller with environment authority", () => {
    mocks.canReadEnvironments = true;
    expect(lockFor(true)).toEqual({ reason: null });
  });

  it("stays locked while the source or grants are unknown", () => {
    expect(lockFor(undefined)).toEqual({
      reason: SOURCE_DESTINATION_UNKNOWN_REASON,
    });
    mocks.rbacLoading = true;
    expect(lockFor(false)).toEqual({
      reason: SOURCE_DESTINATION_UNKNOWN_REASON,
    });
  });
});

function renderWithProviders(ui: React.ReactElement) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>{ui}</TooltipProvider>
    </QueryClientProvider>,
  );
}

function tunnel(environmentLinked: boolean | undefined): TunneledMcpServer {
  return {
    id: "tunnel-1",
    projectId: PROJECT,
    name: "jamf",
    keyPrefix: "gram_tun_",
    environmentLinked,
  } as unknown as TunneledMcpServer;
}

function openRotateDialog() {
  fireEvent.click(screen.getByRole("button", { name: /rotate key/i }));
}

describe("TunnelKeySection", () => {
  it("blocks rotation while a linked server needs environment authority", () => {
    renderWithProviders(<TunnelKeySection tunneledMcpServer={tunnel(true)} />);
    openRotateDialog();
    expect(screen.queryByText("Rotate Tunnel Key")).toBeNull();
  });

  it("allows rotation for a caller with environment authority", () => {
    mocks.canReadEnvironments = true;
    renderWithProviders(<TunnelKeySection tunneledMcpServer={tunnel(true)} />);
    openRotateDialog();
    expect(screen.getByText("Rotate Tunnel Key")).toBeTruthy();
  });

  it("allows rotation when no server on the tunnel is linked", () => {
    renderWithProviders(<TunnelKeySection tunneledMcpServer={tunnel(false)} />);
    openRotateDialog();
    expect(screen.getByText("Rotate Tunnel Key")).toBeTruthy();
  });

  it("disables an open confirmation once the source becomes locked", () => {
    const view = renderWithProviders(
      <TunnelKeySection tunneledMcpServer={tunnel(false)} />,
    );
    openRotateDialog();
    const confirm = () => screen.getByRole("button", { name: /^rotate$/i });
    expect((confirm() as HTMLButtonElement).disabled).toBe(false);

    for (const [linked, reason] of [
      [true, SOURCE_DESTINATION_LOCK_REASON],
      [undefined, SOURCE_DESTINATION_UNKNOWN_REASON],
    ] as const) {
      view.rerender(
        <QueryClientProvider client={new QueryClient()}>
          <TooltipProvider>
            <TunnelKeySection tunneledMcpServer={tunnel(linked)} />
          </TooltipProvider>
        </QueryClientProvider>,
      );
      expect((confirm() as HTMLButtonElement).disabled).toBe(true);
      expect(screen.getByText(reason)).toBeTruthy();
      fireEvent.click(confirm());
      expect(mocks.rotate).not.toHaveBeenCalled();
    }

    // A confirmed unlinked source can be rotated again.
    view.rerender(
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <TunnelKeySection tunneledMcpServer={tunnel(false)} />
        </TooltipProvider>
      </QueryClientProvider>,
    );
    expect((confirm() as HTMLButtonElement).disabled).toBe(false);
  });

  it("disables an open confirmation once mcp:write is lost", () => {
    const view = renderWithProviders(
      <TunnelKeySection tunneledMcpServer={tunnel(false)} />,
    );
    openRotateDialog();
    mocks.canWrite = false;
    view.rerender(
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <TunnelKeySection tunneledMcpServer={tunnel(false)} />
        </TooltipProvider>
      </QueryClientProvider>,
    );
    const confirm = screen.getByRole("button", { name: /^rotate$/i });
    expect((confirm as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(confirm);
    expect(mocks.rotate).not.toHaveBeenCalled();
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
