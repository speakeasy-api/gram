import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useMCPTools } from "./useMCPTools";

const { connect } = vi.hoisted(() => ({
  connect: vi.fn(async (_options: unknown) => ({ tools: async () => ({}) })),
}));

vi.mock("@/lib/mcp-client", () => ({
  createMCPClientWithVersionFallback: connect,
}));
vi.mock("@/elements/lib/mcpSkills", () => ({
  serverServesSkills: () => false,
}));

const auth = {
  isLoading: false as const,
  headers: { "Gram-Chat-Session": "chat-session" },
  ensureValidHeaders: async () => ({}),
};

function setup(url = "https://example.com/mcp/one") {
  const client = new QueryClient();
  const hook = renderHook(
    ({ token }) =>
      useMCPTools({
        auth,
        mcp: undefined,
        mcps: [
          { url, accessToken: token },
          { url: "https://example.com/mcp/two", accessToken: "token-two" },
          { url: "https://example.com/mcp/three", environment: "test" },
        ],
        environment: {},
      }),
    {
      initialProps: { token: "token-one" },
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    },
  );
  return { ...hook, client };
}

describe("useMCPTools per-server bearer tokens", () => {
  beforeEach(() => connect.mockClear());

  it("sends each token only to its server and keeps chat-session auth", async () => {
    const { result } = setup();
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(connect.mock.calls.map(([options]) => options)).toEqual([
      expect.objectContaining({
        transport: expect.objectContaining({
          url: "https://example.com/mcp/one",
          headers: {
            "Gram-Chat-Session": "chat-session",
            Authorization: "Bearer token-one",
          },
        }),
      }),
      expect.objectContaining({
        transport: expect.objectContaining({
          url: "https://example.com/mcp/two",
          headers: {
            "Gram-Chat-Session": "chat-session",
            Authorization: "Bearer token-two",
          },
        }),
      }),
      expect.objectContaining({
        transport: expect.objectContaining({
          url: "https://example.com/mcp/three",
          headers: {
            "Gram-Chat-Session": "chat-session",
            "Gram-Environment": "test",
          },
        }),
      }),
    ]);
  });

  it("rejects a non-HTTPS bearer target without contacting it", async () => {
    const warn = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      const { result } = setup("http://example.com/mcp/one");
      await waitFor(() => expect(result.current.isSuccess).toBe(true));
      expect(connect).toHaveBeenCalledTimes(2);
      expect(connect).not.toHaveBeenCalledWith(
        expect.objectContaining({
          transport: expect.objectContaining({
            url: "http://example.com/mcp/one",
          }),
        }),
      );
    } finally {
      warn.mockRestore();
    }
  });

  it("reconnects when a bearer token is refreshed", async () => {
    const { result, rerender, client } = setup();
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(
      JSON.stringify(
        client
          .getQueryCache()
          .getAll()
          .map((query) => query.queryKey),
      ),
    ).not.toContain("token-one");
    rerender({ token: "refreshed-token" });
    await waitFor(() =>
      expect(connect).toHaveBeenCalledWith(
        expect.objectContaining({
          transport: expect.objectContaining({
            url: "https://example.com/mcp/one",
            headers: expect.objectContaining({
              Authorization: "Bearer refreshed-token",
            }),
          }),
        }),
      ),
    );
  });
});
