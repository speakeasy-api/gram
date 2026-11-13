import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useCreateRemoteMcpSource } from "./hooks";

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  configureGitHub: vi.fn(),
  update: vi.fn(),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "fixture" }),
  useSdkClient: () => ({
    remoteMcp: { createServerAndMcpServer: mocks.create },
    authClients: { configureGitHubApp: mocks.configureGitHub },
    mcpServers: { update: mocks.update },
  }),
}));
vi.mock("@/lib/mcpEndpoints", () => ({
  createDefaultMcpEndpoint: vi.fn().mockResolvedValue(undefined),
  DEFAULT_ENDPOINT_FAILED_MESSAGE: "Endpoint setup failed",
}));

const mcpServer = {
  id: "server-fixture",
  name: "Fixture",
  visibility: "disabled",
};
const githubApp = {
  instanceUrl: "https://github.com",
  appId: "123",
  installationId: "456",
  privateKey: "-----BEGIN PRIVATE KEY-----",
};
const keys = [
  ["launcher-auth-clients"],
  ["@gram/client", "authClients", "list"],
  ["@gram/client", "authClients", "get"],
  ["@gram/client", "authClients", "getAudience"],
  ["@gram/client", "authClients", "getMcpServerAuthentication"],
  ["@gram/client", "remoteSessions", "listServerClientAssociations"],
];
function renderCreation() {
  const queryClient = new QueryClient();
  for (const key of keys) queryClient.setQueryData(key, { cached: true });
  const hook = renderHook(() => useCreateRemoteMcpSource(), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  });
  return { ...hook, queryClient };
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.create.mockResolvedValue({
    remoteMcpServer: {
      id: "remote-fixture",
      url: "https://proxy.example.test/mcp",
    },
    mcpServer,
  });
  mocks.configureGitHub.mockResolvedValue({ id: "app-fixture" });
  mocks.update.mockResolvedValue({ ...mcpServer, visibility: "private" });
});

describe("remote MCP creation auth-client cache coherence", () => {
  it.each(["success", "visibility failure", "configuration failure"])(
    "invalidates auth-client views after GitHub App submission with %s",
    async (outcome) => {
      if (outcome === "visibility failure")
        mocks.update.mockRejectedValue(new Error("visibility failed"));
      if (outcome === "configuration failure")
        mocks.configureGitHub.mockRejectedValue(new Error("configure failed"));
      const { result, queryClient } = renderCreation();
      await act(async () => {
        const data = await result.current.mutateAsync({
          url: "https://proxy.example.test/mcp",
          identityMode: "agent",
          githubApp,
        });
        expect(data.identityConfiguration.status).toBe(
          outcome === "success" ? "configured" : "setup-required",
        );
      });
      expect(mocks.configureGitHub).toHaveBeenCalledOnce();
      for (const key of keys)
        expect(queryClient.getQueryState(key)?.isInvalidated).toBe(true);
    },
  );

  it("does not refresh auth-client views when no GitHub App was submitted", async () => {
    const { result, queryClient } = renderCreation();
    await act(async () => {
      await result.current.mutateAsync({
        url: "https://proxy.example.test/mcp",
        identityMode: "agent",
      });
    });
    expect(mocks.configureGitHub).not.toHaveBeenCalled();
    for (const key of keys)
      expect(queryClient.getQueryState(key)?.isInvalidated).toBe(false);
  });
});
