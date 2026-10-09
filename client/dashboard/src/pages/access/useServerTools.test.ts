import { describe, expect, it, vi } from "vitest";
import type { Server, ServerTool } from "./serverMerge";
import { resolveToolSource, type ToolSourceInputs } from "./useServerTools";

const tunneled: Server = {
  id: "srv-1",
  name: "JAMF",
  slug: "jamf",
  tools: [],
  dynamicTools: true,
  storedToolInventory: true,
  tunneledSourceId: "tun-1",
};

const listDevices: ServerTool = {
  id: "srv-1:list_devices",
  name: "list_devices",
  type: "proxy",
};

function inputs(overrides: Partial<ToolSourceInputs> = {}): ToolSourceInputs {
  return {
    server: tunneled,
    stored: {
      isLoading: false,
      isError: false,
      tools: [],
      retry: vi.fn<() => void>(),
    },
    canRecordTools: true,
    target: { isLoading: false, isError: false, retry: vi.fn<() => void>() },
    platformSlug: "jamf",
    live: {
      loading: false,
      needsAuth: false,
      isError: false,
      tools: undefined,
      connect: undefined,
      retry: vi.fn<() => void>(),
    },
    tunnel: { offline: false, retry: vi.fn<() => void>() },
    ...overrides,
  };
}

describe("resolveToolSource", () => {
  it("lists stored tools whether or not the tunnel is offline", () => {
    const source = resolveToolSource(
      inputs({
        stored: {
          isLoading: false,
          isError: false,
          tools: [listDevices],
          retry: vi.fn<() => void>(),
        },
        tunnel: { offline: true, retry: vi.fn<() => void>() },
      }),
    );
    expect(source).toEqual({ status: "ready", tools: [listDevices] });
  });

  it("reports an offline tunnel when nothing is stored and listing failed", () => {
    const retry = vi.fn<() => void>();
    const source = resolveToolSource(
      inputs({
        live: {
          loading: false,
          needsAuth: false,
          isError: true,
          tools: undefined,
          connect: undefined,
          retry: vi.fn<() => void>(),
        },
        tunnel: { offline: true, retry },
      }),
    );
    expect(source.status).toBe("offline");
    if (source.status !== "offline") return;
    source.retry();
    expect(retry).toHaveBeenCalledOnce();
  });

  it("prefers a listing that succeeded over an offline status read", () => {
    const source = resolveToolSource(
      inputs({
        live: {
          loading: false,
          needsAuth: false,
          isError: false,
          tools: [listDevices],
          connect: undefined,
          retry: vi.fn<() => void>(),
        },
        tunnel: { offline: true, retry: vi.fn<() => void>() },
      }),
    );
    expect(source).toEqual({ status: "ready", tools: [listDevices] });
  });

  it("treats a failed listing with unknown tunnel status as a plain error", () => {
    // A status read the caller may not make (403) or that failed leaves
    // `offline` false: nothing says the agent is gone.
    const retry = vi.fn<() => void>();
    const source = resolveToolSource(
      inputs({
        live: {
          loading: false,
          needsAuth: false,
          isError: true,
          tools: undefined,
          connect: undefined,
          retry,
        },
      }),
    );
    expect(source).toEqual({ status: "error", retry });
  });

  it("does not present tools kept from a listing whose refetch failed", () => {
    const retry = vi.fn<() => void>();
    const source = resolveToolSource(
      inputs({
        live: {
          loading: false,
          needsAuth: false,
          isError: true,
          tools: [listDevices],
          connect: undefined,
          retry,
        },
      }),
    );
    expect(source).toEqual({ status: "error", retry });
  });

  it("does not present a cached listing while the session is still loading", () => {
    // The user-session token is still minting, so the cached listing says
    // nothing about what this session can list.
    const source = resolveToolSource(
      inputs({
        live: {
          loading: true,
          needsAuth: false,
          isError: false,
          tools: [listDevices],
          connect: undefined,
          retry: vi.fn<() => void>(),
        },
      }),
    );
    expect(source).toEqual({ status: "loading" });
  });

  it("asks for write access before opening a live session", () => {
    const source = resolveToolSource(
      inputs({
        canRecordTools: false,
        tunnel: { offline: true, retry: vi.fn<() => void>() },
      }),
    );
    expect(source).toEqual({ status: "needs-write" });
  });

  it("keeps unproxied servers out of tool-level limits", () => {
    const source = resolveToolSource(
      inputs({
        server: {
          ...tunneled,
          storedToolInventory: false,
          tunneledSourceId: undefined,
        },
      }),
    );
    expect(source).toEqual({ status: "dynamic" });
  });
});
