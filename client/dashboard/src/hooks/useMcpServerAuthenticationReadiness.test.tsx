import { renderHook } from "@testing-library/react";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  github: vi.fn(),
  probe: vi.fn(),
  hasScope: vi.fn(),
  rbacLoading: false,
  enforcement: "legacy",
  auth: {
    data: undefined as { status: string } | undefined,
    isPending: false,
    isError: false,
  },
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: mocks.hasScope, isLoading: mocks.rbacLoading }),
}));
vi.mock("@/hooks/useUpstreamAuthEnforcement", () => ({
  useUpstreamAuthEnforcement: () => mocks.enforcement,
}));
vi.mock("@/lib/remote-identity/drafts/useGitHubAppDraft", () => ({
  useBoundGitHubApp: mocks.github,
}));
vi.mock("@gram/client/react-query/mcpServerAuthentication.js", () => ({
  useMcpServerAuthentication: () => mocks.auth,
}));
vi.mock("@gram/client/react-query/remoteMcpServerHeaders.js", () => ({
  useRemoteMcpServerHeaders: () => ({
    data: { headers: [] },
    isLoading: false,
    isError: false,
  }),
}));
vi.mock("@/lib/remote-identity", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/remote-identity")>()),
  useAllRemoteSessionClients: () => ({
    items: [],
    isLoading: false,
    isError: false,
  }),
  useUpstreamProbe: mocks.probe,
}));

import { useMcpServerAuthenticationReadiness } from "./useMcpServerAuthenticationReadiness";

const server = {
  id: "server-fixture",
  remoteMcpServerId: "remote-fixture",
} as McpServer;

describe("shared authentication readiness with GitHub App identity", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.enforcement = "legacy";
    mocks.rbacLoading = false;
    mocks.hasScope.mockReturnValue(true);
    mocks.github.mockReturnValue({
      bound: undefined,
      loading: false,
      error: false,
    });
    mocks.probe.mockReturnValue("idle");
    mocks.auth = { data: undefined, isPending: false, isError: false };
  });

  it("reports a bound GitHub App as a ready shared credential without probing", () => {
    mocks.github.mockReturnValue({
      bound: { id: "client-fixture", enabled: true },
      loading: false,
      error: false,
    });
    const { result } = renderHook(() =>
      useMcpServerAuthenticationReadiness(server),
    );
    expect(mocks.github).toHaveBeenCalledWith(server.id, true);
    expect(result.current).toMatchObject({
      ready: true,
      loading: false,
      unavailable: false,
      remoteIdentityMode: "agent",
    });
    expect(result.current.description).toContain("shared credential");
    expect(mocks.probe).toHaveBeenCalledWith("remote-fixture", false);
  });

  it("treats a disabled binding as unavailable rather than anonymous", () => {
    mocks.github.mockReturnValue({
      bound: { enabled: false },
      loading: false,
      error: false,
    });
    const { result } = renderHook(() =>
      useMcpServerAuthenticationReadiness(server),
    );
    expect(result.current).toMatchObject({
      ready: false,
      unavailable: true,
      remoteIdentityMode: "none",
    });
    expect(mocks.probe).toHaveBeenCalledWith("remote-fixture", false);
  });

  it("probes an unbound server once identity queries settle", () => {
    renderHook(() => useMcpServerAuthenticationReadiness(server));
    expect(mocks.probe).toHaveBeenCalledWith("remote-fixture", true);
  });

  it.each([
    { loading: true, error: false },
    { loading: false, error: true },
  ])("does not probe while GitHub state is unresolved: %o", (state) => {
    mocks.github.mockReturnValue({ bound: undefined, ...state });
    const { result } = renderHook(() =>
      useMcpServerAuthenticationReadiness(server),
    );
    expect(result.current.loading).toBe(state.loading);
    expect(result.current.unavailable).toBe(state.error);
    expect(result.current.ready).toBe(false);
    expect(mocks.probe).toHaveBeenCalledWith("remote-fixture", false);
  });

  it("gates the GitHub lookup on organization read access", () => {
    mocks.hasScope.mockReturnValue(false);
    const { result } = renderHook(() =>
      useMcpServerAuthenticationReadiness(server),
    );
    expect(result.current.unavailable).toBe(true);
    expect(mocks.probe).toHaveBeenCalledWith("remote-fixture", false);
    expect(mocks.hasScope).toHaveBeenCalledWith("org:read");
    expect(mocks.github).toHaveBeenCalledWith(server.id, false);
  });

  it("waits for access scopes before loading or probing legacy identity", () => {
    mocks.rbacLoading = true;
    const { result } = renderHook(() =>
      useMcpServerAuthenticationReadiness(server),
    );
    expect(result.current.loading).toBe(true);
    expect(mocks.github).toHaveBeenCalledWith(server.id, false);
    expect(mocks.probe).toHaveBeenCalledWith("remote-fixture", false);
  });

  it("keeps enforced readiness on the credentials API without a legacy GitHub lookup", () => {
    mocks.enforcement = "enforced";
    mocks.auth.data = { status: "configured" };
    const { result } = renderHook(() =>
      useMcpServerAuthenticationReadiness(server),
    );
    expect(mocks.github).toHaveBeenCalledWith(server.id, false);
    expect(result.current.ready).toBe(true);
    expect(result.current.description).toBe(
      "Credentials are set up for the upstream service.",
    );
  });
});
