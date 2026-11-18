import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { useGatewayMemberSignIn } from "./useGatewayMemberStates";

const rpc = vi.hoisted(() => ({ result: undefined as unknown, calls: 0 }));
vi.mock("./useGatewayInspection", () => ({
  authCacheKey: () => "key",
  gatewayRPC: () => {
    rpc.calls += 1;
    return Promise.resolve(rpc.result);
  },
}));

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function status(audience?: string) {
  return {
    structuredContent: {
      connectUrl: "https://gram.example/mcp/gateway/connect",
      audience,
      servers: [
        { slug: "linear", status: "not_connected" },
        { slug: "slack", status: "connected" },
        { slug: "ops", status: "not_required" },
      ],
    },
  };
}

afterEach(() => {
  rpc.calls = 0;
  vi.unstubAllGlobals();
});

it("names only members the viewer has to connect, and opens the connect page", async () => {
  rpc.result = status();
  const open = vi.fn();
  vi.stubGlobal("open", open);
  const { result } = renderHook(
    () =>
      useGatewayMemberSignIn("https://gram.example/mcp/gateway", undefined, {
        enabled: true,
      }),
    { wrapper },
  );

  await waitFor(() => expect(result.current.needsSignIn("linear")).toBe(true));
  expect(result.current.needsSignIn("slack")).toBe(false);
  expect(result.current.needsSignIn("ops")).toBe(false);
  result.current.connect();
  expect(open).toHaveBeenCalledWith(
    "https://gram.example/mcp/gateway/connect",
    "_blank",
    "noopener,noreferrer",
  );
});

it("checks again and refreshes the caller when the viewer comes back", async () => {
  rpc.result = status();
  const onConnected = vi.fn<() => void>();
  const { result } = renderHook(
    () =>
      useGatewayMemberSignIn("https://gram.example/mcp/gateway", undefined, {
        enabled: true,
        onConnected,
      }),
    { wrapper },
  );
  await waitFor(() => expect(result.current.needsSignIn("linear")).toBe(true));

  rpc.result = {
    structuredContent: {
      ...status().structuredContent,
      servers: [{ slug: "linear", status: "connected" }],
    },
  };
  act(() => {
    window.dispatchEvent(new Event("focus"));
  });
  await waitFor(() => expect(onConnected).toHaveBeenCalled());
  expect(result.current.needsSignIn("linear")).toBe(false);
});

it("offers nothing when someone else signs in for the caller", async () => {
  rpc.result = status("agent_editors");
  const { result } = renderHook(
    () =>
      useGatewayMemberSignIn("https://gram.example/mcp/gateway", undefined, {
        enabled: true,
      }),
    { wrapper },
  );
  await waitFor(() => expect(rpc.calls).toBe(1));
  expect(result.current.needsSignIn("linear")).toBe(false);
});
