import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useMcpServerReachability } from "./useMcpServerReachability";

const listing = vi.hoisted(() => vi.fn());
const mint = vi.hoisted(() => vi.fn());
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({
    user: { id: "viewer" },
    organization: { id: "org" },
  }),
}));
vi.mock("@/hooks/useUserSessionToken", () => ({
  useUserSessionToken: mint,
}));
vi.mock("@/hooks/useProxiedMcpTools", () => ({
  useProxiedMcpTools: listing,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => false }),
}));
vi.mock("@/hooks/useToolMetadata", () => ({
  useToolMetadata: () => ({ metadataByTool: {} }),
}));
const chatSession = vi.hoisted(() =>
  vi.fn(() => ({ headers: undefined, error: null })),
);
vi.mock("@/pages/mcp/inspect/useChatSessionHeaders", () => ({
  useChatSessionHeaders: chatSession,
}));
vi.mock("@/lib/utils", () => ({
  getServerURL: () => "https://app.example",
  mcpConnectionUrl: (url: string | undefined) => url,
}));

const options = {
  mcpUrl: "https://app.example/mcp/server",
  mcpServerId: "server",
  projectId: "project",
  userSessionIssuerId: "issuer",
  platformSlug: "server",
  publicTunnel: false,
  enabled: true,
};
const grantKey = `inspect-oauth:${JSON.stringify(["org", "viewer", "issuer", "server", "https://app.example/mcp/server"])}`;

beforeEach(() => {
  sessionStorage.clear();
  listing.mockReturnValue({ tools: undefined, needsAuth: false });
});

afterEach(() => {
  cleanup();
  listing.mockReset();
  mint.mockReset();
  chatSession.mockClear();
});

describe("useMcpServerReachability", () => {
  it("does not create a session for an issuer-gated server the viewer has not connected", () => {
    const { result } = renderHook(() => useMcpServerReachability(options));

    expect(mint).not.toHaveBeenCalled();
    expect(listing).toHaveBeenLastCalledWith(
      "https://app.example/mcp/server",
      expect.objectContaining({ enabled: false, headers: undefined }),
    );
    expect(result.current).toEqual({ kind: "unverified" });
  });

  it("probes with the grant the viewer made in Inspect", () => {
    sessionStorage.setItem(
      grantKey,
      JSON.stringify({ accessToken: "token", expiresAt: Date.now() + 60_000 }),
    );
    listing.mockReturnValue({ tools: {}, needsAuth: false });

    const { result } = renderHook(() => useMcpServerReachability(options));

    expect(mint).not.toHaveBeenCalled();
    expect(listing).toHaveBeenLastCalledWith(
      "https://app.example/mcp/server",
      expect.objectContaining({
        enabled: true,
        headers: { Authorization: "Bearer token" },
      }),
    );
    expect(result.current).toEqual({ kind: "reachable", driftCount: 0 });
  });

  it("ignores a cached listing when the viewer has no grant", () => {
    listing.mockReturnValue({ tools: {}, needsAuth: false });

    const { result } = renderHook(() => useMcpServerReachability(options));

    expect(result.current).toEqual({ kind: "unverified" });
  });

  it.each([
    ["with", "issuer"],
    ["without", undefined],
  ])("probes a public tunnel %s an issuer anonymously", (_, issuer) => {
    renderHook(() =>
      useMcpServerReachability({
        ...options,
        userSessionIssuerId: issuer,
        publicTunnel: true,
      }),
    );

    expect(chatSession).toHaveBeenLastCalledWith(false);
    expect(listing).toHaveBeenLastCalledWith(
      "https://app.example/mcp/server",
      expect.objectContaining({ enabled: true, headers: undefined }),
    );
  });
});
