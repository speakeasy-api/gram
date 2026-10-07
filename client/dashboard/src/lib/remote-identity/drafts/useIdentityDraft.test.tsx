import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as setupPolicies from "../setup/oauthPolicies";
import { SLACK_DEFAULT_SCOPES } from "../setup/slack";
import { useUserIdentityDraft } from "./useIdentityDraft";

const mocks = vi.hoisted(() => ({
  issuers: [] as RemoteSessionIssuer[],
  clients: [] as RemoteSessionClient[],
  commit: vi.fn(),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    remoteSessions: { commitServerIdentityConfiguration: mocks.commit },
    remoteMcp: { discoverProtectedResourceMetadata: vi.fn() },
  }),
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}));
vi.mock("@gram/client/react-query/remoteSessionIssuers.js", () => ({
  useRemoteSessionIssuers: () => ({
    data: { result: { items: mocks.issuers } },
  }),
  useRemoteSessionIssuersInfinite: () => ({
    data: { pages: [{ result: { items: mocks.issuers } }] },
    fetchNextPage: vi.fn(),
  }),
  invalidateAllRemoteSessionIssuers: vi.fn(),
}));
vi.mock("@gram/client/react-query/remoteSessionClients.js", () => ({
  invalidateAllRemoteSessionClients: vi.fn(),
}));
vi.mock("@gram/client/react-query/remoteSessionsCount.js", () => ({
  useRemoteSessionsCount: () => ({}),
  invalidateAllRemoteSessionsCount: vi.fn(),
}));
vi.mock("../queries/useAllRemoteSessionClients", () => ({
  useAllRemoteSessionClients: (filters: { remoteSessionIssuerId: string }) => ({
    items: mocks.clients.filter(
      (client) =>
        client.remoteSessionIssuerId === filters.remoteSessionIssuerId,
    ),
    isLoading: false,
  }),
}));
vi.mock("../queries/useRemoteSessionIssuersByIds", () => ({
  useRemoteSessionIssuersByIds: () => ({ items: mocks.issuers }),
}));
vi.mock("../queries/useProtectedResourceMetadata", () => ({
  useProtectedResourceMetadata: () => ({ status: "idle", metadata: null }),
}));

const slackIssuer = {
  id: "provider-slack",
  slug: "slack",
  issuer: "https://mcp.slack.com",
  authorizationEndpoint: "https://slack.com/oauth/v2_user/authorize",
  tokenEndpoint: "https://slack.com/api/oauth.v2.user.access",
  tokenEndpointAuthMethodsSupported: ["client_secret_post"],
} as RemoteSessionIssuer;
const reusableClient = {
  id: "client-existing",
  clientId: "existing-app",
  remoteSessionIssuerId: slackIssuer.id,
  scope: SLACK_DEFAULT_SCOPES,
  tokenEndpointAuthMethod: "client_secret_post",
  legacyCallbackUrl: false,
  createdAt: new Date(0),
} as RemoteSessionClient;

function draft(
  linkedClients: RemoteSessionClient[] = [],
  upstreamUrl = "https://mcp.slack.com/mcp",
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderHook(
    () =>
      useUserIdentityDraft({
        mcpServerId: "server-test",
        remoteMcpServerId: "remote-test",
        upstreamUrl,
        linkedClients,
        configured: linkedClients.length > 0,
        enabled: true,
      }),
    {
      wrapper: ({ children }) => (
        <QueryClientProvider client={queryClient}>
          {children}
        </QueryClientProvider>
      ),
    },
  );
}

beforeEach(() => {
  mocks.issuers = [{ ...slackIssuer }];
  mocks.clients = [reusableClient];
  mocks.commit.mockReset().mockResolvedValue({});
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("Slack identity draft", () => {
  it("starts with a new app even when a reusable client exists", () => {
    const { result } = draft();
    expect(result.current.choice).toBe("manual");
    expect(result.current.existingAvailable).toBe(true);
    expect(result.current.canSave).toBe(false);
    act(() => result.current.selectChoice("existing"));
    expect(result.current.existingClientId).toBe(reusableClient.id);
    expect(result.current.canSave).toBe(true);
  });

  it("preserves pasted credentials and allows repeated supported access updates", async () => {
    const { result } = draft();
    act(() => {
      result.current.setClientId("pasted-id");
      result.current.setClientSecret("pasted-secret");
    });
    expect(result.current.canSave).toBe(false);
    expect(result.current.guidedSetup?.canApplyDefaults).toBe(true);
    act(() => result.current.guidedSetup?.applyDefaults(SLACK_DEFAULT_SCOPES));
    expect(result.current.guidedSetup?.manualActive).toBe(true);
    expect(result.current.canSave).toBe(true);
    expect(result.current.guidedSetup?.canApplyDefaults).toBe(true);
    const scopes = ["search:read.private"];
    act(() => result.current.guidedSetup?.applyDefaults(scopes));
    expect(result.current.scopes).toEqual(scopes);
    expect(result.current.clientId).toBe("pasted-id");
    expect(result.current.clientSecret).toBe("pasted-secret");
    await act(async () => {
      await result.current.save();
    });
    expect(mocks.commit).toHaveBeenCalledWith({
      commitServerIdentityConfigurationForm: expect.objectContaining({
        clientMode: "manual",
        initialBindingOnly: true,
        clientConfiguration: expect.objectContaining({
          clientId: "pasted-id",
          clientSecret: "pasted-secret",
          scope: scopes,
          tokenEndpointAuthMethod: "client_secret_post",
        }),
      }),
    });
  });

  it.each([["chat:write"], ["channels:read"]])(
    "does not overwrite unsupported or partial raw scopes %j",
    (scope) => {
      const { result } = draft();
      act(() => result.current.setScopes([scope]));
      expect(result.current.guidedSetup?.canApplyDefaults).toBe(false);
      act(() =>
        result.current.guidedSetup?.applyDefaults(SLACK_DEFAULT_SCOPES),
      );
      expect(result.current.scopes).toEqual([scope]);
      expect(result.current.guidedSetup?.manualActive).toBe(false);
    },
  );

  it("does not accept unsupported selected defaults", () => {
    const { result } = draft();
    act(() => result.current.guidedSetup?.applyDefaults(["chat:write"]));
    expect(result.current.scopes).toEqual([]);
    expect(result.current.guidedSetup?.manualActive).toBe(false);
  });

  it("requires a secret and supported scopes even before guided defaults", async () => {
    const { result } = draft();
    act(() => result.current.setClientId("manual-id"));
    expect(result.current.canSave).toBe(false);
    await act(async () => {
      await result.current.save();
    });
    expect(mocks.commit).not.toHaveBeenCalled();
    act(() => result.current.setScopes(SLACK_DEFAULT_SCOPES));
    expect(result.current.canSave).toBe(false);
    act(() => result.current.setClientSecret("secret"));
    expect(result.current.canSave).toBe(true);
    act(() => result.current.setScopes(["chat:write"]));
    expect(result.current.canSave).toBe(false);
    await act(async () => {
      await result.current.save();
    });
    expect(mocks.commit).not.toHaveBeenCalled();
  });

  it("requires the reviewed provider even with credentials and supported scopes", async () => {
    mocks.issuers = [
      { ...slackIssuer, tokenEndpoint: "https://slack.com/wrong" },
    ];
    const { result } = draft();
    act(() => {
      result.current.setClientId("manual-id");
      result.current.setClientSecret("secret");
      result.current.setScopes(SLACK_DEFAULT_SCOPES);
    });
    expect(result.current.guidedSetup?.canApplyDefaults).toBe(false);
    expect(result.current.canSave).toBe(false);
    await act(async () => {
      await result.current.save();
    });
    expect(mocks.commit).not.toHaveBeenCalled();
  });

  it("blocks defaults while connected and does not mark a replacement initial-only", async () => {
    const { result } = draft([reusableClient]);
    expect(result.current.connected).toBe(true);
    expect(result.current.guidedSetup?.canApplyDefaults).toBe(false);
    act(() => result.current.guidedSetup?.applyDefaults(SLACK_DEFAULT_SCOPES));
    expect(result.current.scopes).toEqual([]);
    act(() => result.current.clear());
    act(() => result.current.guidedSetup?.applyDefaults(SLACK_DEFAULT_SCOPES));
    act(() => {
      result.current.setClientId("replacement-id");
      result.current.setClientSecret("secret");
    });
    await act(async () => {
      await result.current.save();
    });
    expect(
      mocks.commit.mock.calls[0]?.[0].commitServerIdentityConfigurationForm
        .initialBindingOnly,
    ).toBeUndefined();
  });

  it("blocks defaults while a save is pending", async () => {
    let resolveCommit!: (value: object) => void;
    mocks.commit.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveCommit = resolve;
        }),
    );
    const { result } = draft();
    act(() => result.current.guidedSetup?.applyDefaults(SLACK_DEFAULT_SCOPES));
    act(() => {
      result.current.setClientId("manual-id");
      result.current.setClientSecret("secret");
    });
    let saving!: Promise<void>;
    act(() => {
      saving = result.current.save();
    });
    await waitFor(() => expect(result.current.saving).toBe(true));
    expect(result.current.guidedSetup?.canApplyDefaults).toBe(false);
    act(() =>
      result.current.guidedSetup?.applyDefaults(["search:read.private"]),
    );
    expect(result.current.scopes).toEqual(SLACK_DEFAULT_SCOPES);
    await act(async () => {
      resolveCommit({});
      await saving;
    });
  });

  it("keeps non-Slack reuse and public manual client semantics", () => {
    mocks.issuers = [
      {
        ...slackIssuer,
        id: "provider-example",
        issuer: "https://example.test",
        tokenEndpointAuthMethodsSupported: ["none"],
      },
    ];
    mocks.clients.push({
      ...reusableClient,
      id: "client-example",
      remoteSessionIssuerId: "provider-example",
      tokenEndpointAuthMethod: "none",
    });
    const { result } = draft([], "https://example.test/mcp");
    expect(result.current.guidedSetup).toBeUndefined();
    expect(result.current.choice).toBe("existing");
    act(() => result.current.selectChoice("manual"));
    act(() => result.current.setClientId("public-client"));
    expect(result.current.canSave).toBe(true);
  });
});

describe("provider-independent guided setup", () => {
  it("uses another policy's scopes, labels and public-client authentication", async () => {
    const policy: setupPolicies.OAuthSetupPolicy = {
      id: "example",
      providerName: "Example",
      changeConnectionLabel: "Change Example application",
      matchesUrl: (url) => url === "https://example.test/mcp",
      prefersManualRegistration: true,
      requiresClientSecret: false,
      tokenEndpointAuthMethod: "none",
      isProviderCompatible: (provider) =>
        provider?.issuer === "https://example.test",
      isScopeSelectionCompatible: (scopes) =>
        scopes?.length === 1 && scopes[0] === "example:read",
      clientMismatch: () => "Unsupported client",
      manualConfigurationError: "Choose Example read access",
    };
    vi.spyOn(setupPolicies, "getOAuthSetupPolicy").mockReturnValue(policy);
    mocks.issuers = [
      {
        ...slackIssuer,
        id: "provider-example",
        issuer: "https://example.test",
      },
    ];
    const { result } = draft([], "https://example.test/mcp");
    expect(result.current.guidedSetup).toMatchObject({
      id: "example",
      providerName: "Example",
      requiresClientSecret: false,
      changeConnectionLabel: "Change Example application",
    });
    act(() => result.current.guidedSetup?.applyDefaults(SLACK_DEFAULT_SCOPES));
    expect(result.current.scopes).toEqual([]);
    act(() => {
      result.current.guidedSetup?.applyDefaults(["example:read"]);
      result.current.setClientId("example-public-client");
    });
    expect(result.current.canSave).toBe(true);
    await act(async () => {
      await result.current.save();
    });
    expect(mocks.commit).toHaveBeenCalledWith({
      commitServerIdentityConfigurationForm: expect.objectContaining({
        clientMode: "manual",
        clientConfiguration: expect.objectContaining({
          clientId: "example-public-client",
          clientSecret: undefined,
          scope: ["example:read"],
          tokenEndpointAuthMethod: "none",
        }),
      }),
    });
  });
});
