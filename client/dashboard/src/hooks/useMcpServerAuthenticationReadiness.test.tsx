import { renderHook } from "@testing-library/react";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  associations: vi.fn(),
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
vi.mock("@gram/client/react-query/serverClientAssociations.js", () => ({
  useServerClientAssociations: mocks.associations,
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
  const noApp = {
    githubAppBound: false,
    githubAppEnabled: false,
    githubAppRequired: false,
  };
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.enforcement = "legacy";
    mocks.rbacLoading = false;
    mocks.hasScope.mockReturnValue(true);
    mocks.associations.mockReturnValue({
      data: noApp,
      isLoading: false,
      isError: false,
    });
    mocks.probe.mockReturnValue("idle");
    mocks.auth = { data: undefined, isPending: false, isError: false };
  });

  it("reports an enabled bound App as ready without organization read access or probing", () => {
    mocks.hasScope.mockImplementation((scope: string) => scope === "mcp:read");
    mocks.associations.mockReturnValue({
      data: {
        githubAppBound: true,
        githubAppEnabled: true,
        githubAppRequired: true,
      },
    });
    const { result } = renderHook(() =>
      useMcpServerAuthenticationReadiness(server),
    );
    expect(result.current).toMatchObject({
      ready: true,
      loading: false,
      unavailable: false,
      remoteIdentityMode: "agent",
    });
    expect(mocks.hasScope).not.toHaveBeenCalledWith("org:read");
    expect(mocks.probe).toHaveBeenCalledWith("remote-fixture", false);
  });

  it.each([
    { githubAppBound: true, githubAppEnabled: false, githubAppRequired: true },
    { githubAppBound: false, githubAppEnabled: false, githubAppRequired: true },
    {},
  ])(
    "does not probe a disabled, historical, or unknown App requirement: %o",
    (data) => {
      mocks.associations.mockReturnValue({ data });
      const { result } = renderHook(() =>
        useMcpServerAuthenticationReadiness(server),
      );
      expect(result.current).toMatchObject({
        ready: false,
        unavailable: true,
        remoteIdentityMode: "none",
      });
      expect(mocks.probe).toHaveBeenCalledWith("remote-fixture", false);
    },
  );

  it("probes an unbound server without org:read once scoped metadata settles", () => {
    mocks.hasScope.mockImplementation((scope: string) => scope === "mcp:read");
    const { result } = renderHook(() =>
      useMcpServerAuthenticationReadiness(server),
    );
    expect(result.current.unavailable).toBe(false);
    expect(mocks.probe).toHaveBeenCalledWith("remote-fixture", true);
  });

  it.each([
    { isLoading: true, isError: false },
    { isLoading: false, isError: true },
  ])("does not probe while metadata is unresolved: %o", (state) => {
    mocks.associations.mockReturnValue({ data: undefined, ...state });
    const { result } = renderHook(() =>
      useMcpServerAuthenticationReadiness(server),
    );
    expect(result.current.loading).toBe(state.isLoading);
    expect(result.current.ready).toBe(false);
    expect(mocks.probe).toHaveBeenCalledWith("remote-fixture", false);
  });

  it("keeps missing mcp:read metadata unknown rather than absent", () => {
    mocks.hasScope.mockReturnValue(false);
    mocks.associations.mockReturnValue({ data: undefined });
    const { result } = renderHook(() =>
      useMcpServerAuthenticationReadiness(server),
    );
    expect(result.current.unavailable).toBe(true);
    expect(mocks.associations.mock.calls[0]?.[2]).toMatchObject({
      enabled: false,
    });
  });

  it("waits for access scopes before loading or probing legacy identity", () => {
    mocks.rbacLoading = true;
    const { result } = renderHook(() =>
      useMcpServerAuthenticationReadiness(server),
    );
    expect(result.current.loading).toBe(true);
    expect(mocks.probe).toHaveBeenCalledWith("remote-fixture", false);
  });

  it("keeps enforced readiness on the credentials API", () => {
    mocks.enforcement = "enforced";
    mocks.auth.data = { status: "configured" };
    const { result } = renderHook(() =>
      useMcpServerAuthenticationReadiness(server),
    );
    expect(mocks.associations.mock.calls[0]?.[2]).toMatchObject({
      enabled: false,
    });
    expect(result.current.ready).toBe(true);
    expect(result.current.description).toBe(
      "Credentials are set up for the upstream service.",
    );
  });
});
