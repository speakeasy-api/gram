import { waitForHookState as waitFor } from "@/test/waitForHookState";
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useInspectOAuth } from "./useInspectOAuth";

const auth = vi.hoisted(() => ({ userId: "viewer", orgId: "org" }));
const prepare = vi.hoisted(() => vi.fn());
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({
    user: { id: auth.userId },
    organization: { id: auth.orgId },
  }),
}));
vi.mock("@/lib/inspect-oauth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/inspect-oauth")>()),
  prepareInspectAuthorization: prepare,
}));
vi.mock("@/lib/utils", () => ({ mcpConnectionUrl: (url: string) => url }));

const options = {
  serverUrl: "https://mcp.example/mcp/server",
  issuerId: "issuer",
  serverId: "server",
};
const key = `inspect-oauth:${JSON.stringify(["org", "viewer", "issuer", "server", options.serverUrl])}`;

beforeEach(() => {
  auth.userId = "viewer";
  auth.orgId = "org";
  sessionStorage.clear();
});

afterEach(() => {
  cleanup();
  prepare.mockReset();
  vi.useRealTimers();
});

function seedGrant(duration: number) {
  sessionStorage.setItem(
    key,
    JSON.stringify({ accessToken: "token", expiresAt: Date.now() + duration }),
  );
}

describe("useInspectOAuth", () => {
  it("does not reuse a grant for another viewer, organization, issuer, server, or address", () => {
    seedGrant(60_000);
    const { result, rerender } = renderHook(useInspectOAuth, {
      initialProps: options,
    });
    expect(result.current.accessToken).toBe("token");

    auth.userId = "other-viewer";
    rerender(options);
    expect(result.current.accessToken).toBeUndefined();
    auth.userId = "viewer";
    auth.orgId = "other-org";
    rerender(options);
    expect(result.current.accessToken).toBeUndefined();
    auth.orgId = "org";
    rerender({ ...options, issuerId: "other-issuer" });
    expect(result.current.accessToken).toBeUndefined();
    rerender({ ...options, serverId: "other-server" });
    expect(result.current.accessToken).toBeUndefined();
    rerender({ ...options, serverUrl: "https://another.example/mcp/server" });
    expect(result.current.accessToken).toBeUndefined();
    rerender(options);
    expect(result.current.accessToken).toBe("token");
  });

  it("drops an expired grant without a navigation or caller rerender", () => {
    vi.useFakeTimers();
    seedGrant(1000);
    const { result } = renderHook(() => useInspectOAuth(options));
    expect(result.current.accessToken).toBe("token");
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(result.current.accessToken).toBeUndefined();
    expect(result.current.connect).toBeTypeOf("function");
  });

  it("reports sign-in in progress and recovers when preparation fails", async () => {
    const pending = Promise.withResolvers<string>();
    prepare.mockReturnValueOnce(pending.promise);
    const { result } = renderHook(() => useInspectOAuth(options));
    act(() => result.current.connect?.());
    expect(result.current.isConnecting).toBe(true);
    expect(result.current.error).toBe(false);
    expect(prepare).toHaveBeenCalledWith({
      serverUrl: options.serverUrl,
      tokenUrl: `${options.serverUrl}/token`,
      grantKey: key,
      userId: auth.userId,
    });
    pending.reject(new Error("failed to prepare"));
    await waitFor(() => expect(result.current.error).toBe(true));
    expect(result.current.isConnecting).toBe(false);
  });
});
