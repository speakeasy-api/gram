import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useUserIdentityDraft } from "./useIdentityDraft";

const mocks = vi.hoisted(() => ({
  fetchMetadata: vi.fn(),
  commit: vi.fn(),
  resource: vi.fn(),
  discoverResource: vi.fn(),
  issuers: vi.fn(),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    remoteSessionIssuers: { fetchMetadata: mocks.fetchMetadata },
    remoteSessions: { commitServerIdentityConfiguration: mocks.commit },
    remoteMcp: { discoverProtectedResourceMetadata: mocks.discoverResource },
  }),
}));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), warning: vi.fn() } }));
vi.mock("@gram/client/react-query/remoteSessionIssuers.js", () => ({
  useRemoteSessionIssuers: () => ({ data: { result: { items: [] } } }),
  useRemoteSessionIssuersInfinite: () => ({ data: undefined }),
  invalidateAllRemoteSessionIssuers: vi.fn(),
}));
vi.mock("../queries/useRemoteSessionIssuersByIds", () => ({
  useRemoteSessionIssuersByIds: () => ({ items: mocks.issuers() }),
}));
vi.mock("../queries/useAllRemoteSessionClients", () => ({
  useAllRemoteSessionClients: () => ({ items: [], isLoading: false }),
}));
vi.mock("../queries/useProtectedResourceMetadata", () => ({
  useProtectedResourceMetadata: () => mocks.resource(),
}));
vi.mock("@gram/client/react-query/remoteSessionsCount.js", () => ({
  useRemoteSessionsCount: () => ({}),
  invalidateAllRemoteSessionsCount: vi.fn(),
}));
vi.mock("@gram/client/react-query/remoteSessionClients.js", () => ({
  invalidateAllRemoteSessionClients: vi.fn(),
}));
vi.mock("@gram/client/react-query/getRemoteMcpServerScopes.js", () => ({
  invalidateAllGetRemoteMcpServerScopes: vi.fn(),
}));

const advertised = "https://auth.example.test/";
const corrected = "https://auth.example.test";
const key = (issuer: string) => ["remote-session-issuer-metadata", issuer];
const metadata = (issuer: string) => ({
  issuer,
  authorizationEndpoint: `${issuer}/authorize`,
  tokenEndpoint: `${issuer}/token`,
  registrationEndpoint: `${issuer}/register`,
  scopesSupported: ["issuer.read"],
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function renderDraft() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const props = {
    mcpServerId: "example-server",
    remoteMcpServerId: "example-source",
    upstreamUrl: "https://resource.example.test/mcp",
    linkedClients: [],
    configured: false,
    enabled: true,
  };
  const hook = renderHook((input) => useUserIdentityDraft(input), {
    initialProps: props,
    wrapper: ({ children }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  });
  return { ...hook, queryClient, props };
}

beforeEach(() => {
  vi.resetAllMocks();
  mocks.issuers.mockReturnValue([]);
  mocks.resource.mockReturnValue({
    status: "available",
    metadata: { authorizationServers: [advertised] },
  });
  mocks.fetchMetadata.mockImplementation(
    async ({ fetchIssuerMetadataRequestBody: { issuer } }) => metadata(issuer),
  );
  mocks.commit.mockResolvedValue({ status: "registered" });
  mocks.discoverResource.mockResolvedValue({ available: false });
});
afterEach(cleanup);

describe("unsaved issuer correction", () => {
  it("only validates unchanged editor text after explicit discovery, without changing the applied Save identity", async () => {
    const { result } = renderDraft();
    await waitFor(() =>
      expect(result.current.issuerEditorValidated).toBe(true),
    );
    act(() => result.current.setIssuerEditorText(corrected));
    expect(result.current.issuerEditorValidated).toBe(false);
    expect(result.current.discoveryStatus).toBe("ready");
    expect(result.current.canSave).toBe(true);
    expect(mocks.fetchMetadata).toHaveBeenCalledOnce();
    act(() => result.current.setIssuerEditorText(advertised));
    expect(result.current.issuerEditorValidated).toBe(false);
    const check = deferred<ReturnType<typeof metadata>>();
    mocks.fetchMetadata.mockImplementationOnce(() => check.promise);
    act(() => result.current.discoverIssuer());
    await waitFor(() => expect(result.current.discoveryStatus).toBe("loading"));
    expect(result.current.issuerEditorValidated).toBe(false);
    act(() => result.current.setIssuerEditorText(corrected));
    await act(async () => check.resolve(metadata(advertised)));
    await waitFor(() => expect(result.current.discoveryStatus).toBe("ready"));
    expect(result.current.issuerEditorValidated).toBe(false);
    act(() => result.current.discoverIssuer());
    await waitFor(() =>
      expect(result.current.issuerEditorValidated).toBe(true),
    );
    expect(result.current.issuerEditorText).toBe(corrected);
    expect(mocks.fetchMetadata).toHaveBeenLastCalledWith({
      fetchIssuerMetadataRequestBody: { issuer: corrected },
    });
  });

  it("keeps typing unapplied, isolates exact slash keys and late old results", async () => {
    const old = deferred<ReturnType<typeof metadata>>();
    mocks.fetchMetadata.mockImplementationOnce(() => old.promise);
    const { result, queryClient } = renderDraft();
    await waitFor(() => expect(result.current.discoveryStatus).toBe("loading"));
    act(() => {
      result.current.openIssuerEditor();
      result.current.setIssuerEditorText(corrected);
    });
    expect(mocks.fetchMetadata).toHaveBeenCalledOnce();
    act(() => result.current.discoverIssuer());
    await waitFor(() => expect(result.current.discoveryStatus).toBe("ready"));
    expect(queryClient.getQueryData(key(corrected))).toEqual(
      metadata(corrected),
    );
    await act(async () => old.resolve(metadata(advertised)));
    await waitFor(() =>
      expect(queryClient.getQueryData(key(advertised))).toEqual(
        metadata(advertised),
      ),
    );
    expect(result.current.issuerEditorText).toBe(corrected);
    await act(async () => expect(await result.current.save()).toBe(true));
    expect(mocks.commit).toHaveBeenCalledWith({
      commitServerIdentityConfigurationForm: expect.objectContaining({
        createProvider: expect.objectContaining(metadata(corrected)),
      }),
    });
  });

  it("uses Discover to refetch the same issuer inside stale time and retains input through cached failure and recovery", async () => {
    const { result, queryClient } = renderDraft();
    await waitFor(() => expect(result.current.discoveryStatus).toBe("ready"));
    act(() => {
      result.current.selectChoice("manual");
      result.current.setClientId("EXAMPLE-CLIENT");
      result.current.setClientSecret("EXAMPLE-SECRET");
      result.current.setScopes(["custom.read"]);
    });
    const retry = deferred<ReturnType<typeof metadata>>();
    mocks.fetchMetadata.mockImplementationOnce(() => retry.promise);
    act(() => result.current.discoverIssuer());
    await waitFor(() => expect(result.current.discoveryStatus).toBe("loading"));
    expect(result.current.canSave).toBe(false);
    await act(async () => retry.reject(new Error("synthetic failure")));
    await waitFor(() => expect(result.current.discoveryStatus).toBe("error"));
    expect(queryClient.getQueryData(key(advertised))).toEqual(
      metadata(advertised),
    );
    expect(result.current.automaticAvailable).toBe(false);
    await act(async () => expect(await result.current.save()).toBe(false));
    expect(mocks.commit).not.toHaveBeenCalled();
    expect(mocks.fetchMetadata).toHaveBeenCalledTimes(2);
    act(() => result.current.discoverIssuer());
    await waitFor(() => expect(result.current.canSave).toBe(true));
    expect(result.current).toMatchObject({
      choice: "manual",
      clientId: "EXAMPLE-CLIENT",
      clientSecret: "EXAMPLE-SECRET",
      scopes: ["custom.read"],
    });
    expect(mocks.fetchMetadata).toHaveBeenCalledTimes(3);
  });

  it("clears issuer-bound input and method on correction and Reset, keeping the editor open", async () => {
    const { result } = renderDraft();
    await waitFor(() => expect(result.current.discoveryStatus).toBe("ready"));
    act(() => result.current.openIssuerEditor());
    act(() => {
      result.current.selectChoice("manual");
      result.current.setRegistrationMethod("dcr");
      result.current.selectExisting("old-client");
      result.current.setClientId("EXAMPLE-CLIENT");
      result.current.setClientSecret("EXAMPLE-SECRET");
      result.current.setScopes(["custom.read"]);
      result.current.setIssuerEditorText(corrected);
    });
    act(() => result.current.discoverIssuer());
    await waitFor(() => expect(result.current.discoveryStatus).toBe("ready"));
    expect(result.current).toMatchObject({
      choice: "auto",
      registrationMethod: "cimd",
      existingClientId: null,
      clientId: "",
      clientSecret: "",
      scopes: [],
      issuerEditorOpen: true,
    });
    act(() => {
      result.current.selectChoice("manual");
      result.current.setClientId("EXAMPLE-NEW-CLIENT");
    });
    act(() => result.current.resetIssuer());
    await waitFor(() => expect(result.current.discoveryStatus).toBe("ready"));
    expect(result.current).toMatchObject({
      choice: "auto",
      clientId: "",
      issuerEditorText: advertised,
      issuerEditorOpen: true,
    });
  });

  it.each([
    "relative/path",
    "https:auth.example.test",
    "http://auth.example.test",
    "https://user:password@auth.example.test",
    "https://@auth.example.test",
    "https://auth.example.test?",
    "https://auth.example.test#fragment",
    "ftp://auth.example.test",
    "https://auth.example.test/white space",
  ])("rejects invalid correction %s without fetching", async (issuer) => {
    const { result } = renderDraft();
    await waitFor(() => expect(result.current.discoveryStatus).toBe("ready"));
    act(() => result.current.setIssuerEditorText(issuer));
    act(() => result.current.discoverIssuer());
    expect(result.current.issuerEditorError).toContain(
      "absolute HTTPS issuer URL",
    );
    expect(mocks.fetchMetadata).toHaveBeenCalledOnce();
  });

  it.each([
    "HTTPS://AUTH.example.test:443/Tenant%2fPath/",
    "http://localhost:8080/tenant/",
    "http://127.0.0.1:8080",
    "http://[::1]:8080/",
  ])("trims without serializing valid correction %s", async (issuer) => {
    const { result } = renderDraft();
    await waitFor(() => expect(result.current.discoveryStatus).toBe("ready"));
    act(() => result.current.setIssuerEditorText(`  ${issuer}  `));
    act(() => result.current.discoverIssuer());
    await waitFor(() => expect(result.current.discoveryStatus).toBe("ready"));
    expect(result.current.issuerEditorText).toBe(issuer);
    expect(mocks.fetchMetadata).toHaveBeenLastCalledWith({
      fetchIssuerMetadataRequestBody: { issuer },
    });
  });

  it.each(["source", "upstream", "advertisement"])(
    "does not carry a correction or credentials across a changed %s",
    async (change) => {
      const { result, rerender, props } = renderDraft();
      await waitFor(() => expect(result.current.discoveryStatus).toBe("ready"));
      act(() => result.current.openIssuerEditor());
      act(() => result.current.setIssuerEditorText(corrected));
      act(() => result.current.discoverIssuer());
      await waitFor(() => expect(result.current.discoveryStatus).toBe("ready"));
      act(() => {
        result.current.selectChoice("manual");
        result.current.setClientId("EXAMPLE-CLIENT");
      });
      if (change === "advertisement")
        mocks.resource.mockReturnValue({
          status: "available",
          metadata: { authorizationServers: ["https://other.example.test/"] },
        });
      rerender({
        ...props,
        remoteMcpServerId:
          change === "source" ? "other-source" : props.remoteMcpServerId,
        upstreamUrl:
          change === "upstream"
            ? "https://other-resource.example.test/mcp"
            : props.upstreamUrl,
      });
      expect(result.current.issuerEditorOpen).toBe(false);
      expect(result.current.clientId).toBe("");
      expect(result.current.issuerEditorText).toBe(
        change === "advertisement" ? "https://other.example.test/" : advertised,
      );
    },
  );

  it("rejects a queued inline submission when correction changes before commit", async () => {
    const { result } = renderDraft();
    await waitFor(() => expect(result.current.discoveryStatus).toBe("ready"));
    let saving!: Promise<boolean>;
    act(() => {
      saving = result.current.save();
      result.current.setIssuerEditorText(corrected);
    });
    act(() => result.current.discoverIssuer());
    await act(async () => expect(await saving).toBe(false));
    expect(mocks.commit).not.toHaveBeenCalled();
  });

  it.each([false, true])(
    "rejects a queued saved-provider submission after selection changes (restored: %s)",
    async (restore) => {
      mocks.issuers.mockReturnValue([
        {
          id: "saved-provider",
          name: "Saved provider",
          ...metadata("https://saved.example.test"),
        },
      ]);
      const { result } = renderDraft();
      act(() => result.current.selectProvider("saved-provider"));
      await waitFor(() => expect(result.current.choice).toBe("auto"));
      expect(result.current.canSave).toBe(true);
      let saving!: Promise<boolean>;
      act(() => {
        saving = result.current.save();
        result.current.selectProvider(null);
        if (restore) result.current.selectProvider("saved-provider");
      });
      await act(async () => expect(await saving).toBe(false));
      expect(mocks.commit).not.toHaveBeenCalled();
    },
  );

  it("ignores issuer and provider changes while the commit RPC is active", async () => {
    const commit = deferred<{ status: string }>();
    mocks.commit.mockReturnValue(commit.promise);
    const { result } = renderDraft();
    await waitFor(() => expect(result.current.discoveryStatus).toBe("ready"));
    let saving!: Promise<boolean>;
    act(() => {
      saving = result.current.save();
    });
    await waitFor(() => expect(mocks.commit).toHaveBeenCalledOnce());
    act(() => {
      result.current.setIssuerEditorText(corrected);
      result.current.discoverIssuer();
      result.current.selectProvider("other-provider");
      result.current.retryDiscovery();
    });
    expect(result.current.issuerEditorText).toBe(advertised);
    expect(result.current.selected?.isNew).toBe(true);
    expect(mocks.fetchMetadata).toHaveBeenCalledOnce();
    await act(async () => {
      commit.resolve({ status: "registered" });
      await saving;
    });
  });
});
