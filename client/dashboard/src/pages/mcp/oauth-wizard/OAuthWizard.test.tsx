import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// ---------------------------------------------------------------------------
// Mocks. Set up before importing the component under test.
// ---------------------------------------------------------------------------

const mocks = vi.hoisted(() => {
  return {
    addExternalOAuth: vi.fn().mockResolvedValue(undefined),
    updateExternalOAuth: vi.fn().mockResolvedValue(undefined),
    createUserSessionIssuer: vi.fn().mockResolvedValue({ id: "usi-1" }),
    fetchRemoteSessionIssuerMetadata: vi.fn().mockResolvedValue({}),
    createRemoteSessionIssuer: vi.fn().mockResolvedValue({ id: "rsi-1" }),
    createRemoteSessionClient: vi.fn().mockResolvedValue({ id: "rsc-1" }),
    setToolsetUserSessionIssuer: vi.fn().mockResolvedValue(undefined),
    capture: vi.fn(),
    invalidateAllToolset: vi.fn(),
    invalidateAllGetMcpMetadata: vi.fn(),
    invalidateAllListEnvironments: vi.fn(),
    isFeatureEnabled: vi.fn(() => false),
    productTier: ["pro"] as string[],
  };
});

vi.mock("@gram/client/react-query/toolset.js", () => ({
  invalidateAllToolset: mocks.invalidateAllToolset,
}));

vi.mock("@gram/client/react-query/getMcpMetadata.js", () => ({
  invalidateAllGetMcpMetadata: mocks.invalidateAllGetMcpMetadata,
}));

vi.mock("@gram/client/react-query/listEnvironments.js", () => ({
  invalidateAllListEnvironments: mocks.invalidateAllListEnvironments,
  buildListEnvironmentsQuery: () => ({
    queryKey: ["@gram/client", "environments", "list", {}],
    queryFn: () => Promise.resolve({ environments: [] }),
  }),
}));

vi.mock("@gram/client/react-query/addExternalOAuthServer.js", () => ({
  buildAddExternalOAuthServerMutation: () => ({
    mutationKey: [],
    mutationFn: mocks.addExternalOAuth,
  }),
}));

vi.mock("@gram/client/react-query/updateExternalOAuthServer.js", () => ({
  buildUpdateExternalOAuthServerMutation: () => ({
    mutationKey: [],
    mutationFn: mocks.updateExternalOAuth,
  }),
}));

vi.mock("@gram/client/react-query/createUserSessionIssuer.js", () => ({
  buildCreateUserSessionIssuerMutation: () => ({
    mutationKey: [],
    mutationFn: mocks.createUserSessionIssuer,
  }),
}));

vi.mock("@gram/client/react-query/fetchRemoteSessionIssuerMetadata.js", () => ({
  buildFetchRemoteSessionIssuerMetadataMutation: () => ({
    mutationKey: [],
    mutationFn: mocks.fetchRemoteSessionIssuerMetadata,
  }),
}));

vi.mock("@gram/client/react-query/createRemoteSessionIssuer.js", () => ({
  buildCreateRemoteSessionIssuerMutation: () => ({
    mutationKey: [],
    mutationFn: mocks.createRemoteSessionIssuer,
  }),
}));

vi.mock("@gram/client/react-query/createRemoteSessionClient.js", () => ({
  buildCreateRemoteSessionClientMutation: () => ({
    mutationKey: [],
    mutationFn: mocks.createRemoteSessionClient,
  }),
}));

vi.mock("@gram/client/react-query/setToolsetUserSessionIssuer.js", () => ({
  buildSetToolsetUserSessionIssuerMutation: () => ({
    mutationKey: [],
    mutationFn: mocks.setToolsetUserSessionIssuer,
  }),
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ activeOrganizationId: "org-1" }),
}));

vi.mock("@/contexts/Fetcher", () => ({
  useFetcher: () => ({
    fetch: vi.fn().mockResolvedValue(new Response("{}", { status: 200 })),
  }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({}),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({
    capture: mocks.capture,
    isFeatureEnabled: mocks.isFeatureEnabled,
  }),
}));

vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => mocks.productTier,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    environments: {
      Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
    },
  }),
}));

vi.mock("@/components/FeatureRequestModal", () => ({
  FeatureRequestModal: () => null,
}));

// moonshine bundles dynamic icon imports that don't resolve in vitest. Stub
// it down to plain HTML matching the existing test pattern.
vi.mock("@speakeasy-api/moonshine", () => ({
  Button: ({
    children,
    onClick,
    disabled,
  }: {
    children: ReactNode;
    onClick?: () => void;
    disabled?: boolean;
  }) => (
    <button onClick={onClick} disabled={disabled}>
      {children}
    </button>
  ),
  Stack: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  Badge: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}));

// ---------------------------------------------------------------------------
// Now import the component (after mocks are registered).
// ---------------------------------------------------------------------------

import { ConnectOAuthModal } from "./OAuthWizard";

// ---------------------------------------------------------------------------
// Toolset fixture. Most of the real Toolset shape isn't read by the wizard,
// so we cast a minimal stand-in.
// ---------------------------------------------------------------------------

const toolset = {
  name: "MyToolset",
  slug: "mytoolset",
  mcpSlug: "mytoolset",
  rawTools: [],
  oauthEnablementMetadata: { oauth2SecurityCount: 0 },
} as unknown as Parameters<typeof ConnectOAuthModal>[0]["toolset"];

const oauthToolset = {
  ...toolset,
  rawTools: [
    {
      externalMcpToolDefinition: {
        requiresOauth: true,
        slug: "my-oauth-server",
        name: "proxy",
        registryServerName: "My OAuth Server",
        oauthVersion: "2.1",
        oauthAuthorizationEndpoint: "https://idp.example/oauth/authorize",
        oauthTokenEndpoint: "https://idp.example/oauth/token",
        oauthRegistrationEndpoint: "https://idp.example/oauth/register",
        oauthScopesSupported: ["read"],
      },
    },
  ],
} as unknown as Parameters<typeof ConnectOAuthModal>[0]["toolset"];

function renderWizard(
  props: Partial<Parameters<typeof ConnectOAuthModal>[0]> = {},
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <ConnectOAuthModal
          isOpen
          onClose={() => {}}
          toolsetSlug="mytoolset"
          toolset={toolset}
          {...props}
        />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  for (const fn of Object.values(mocks)) {
    if (typeof fn === "function" && "mockClear" in fn) fn.mockClear();
  }
  mocks.isFeatureEnabled.mockReturnValue(false);
  mocks.productTier = ["pro"];
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("OAuthWizard — rendering", () => {
  it("renders the path selection on initial open", () => {
    renderWizard();
    expect(screen.getByText("Connect OAuth")).toBeTruthy();
    expect(screen.getByRole("button", { name: /OAuth Proxy/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /External OAuth/ })).toBeTruthy();
  });

  it("keeps auto-configure labeled as OAuth Proxy when user-session onboarding is enabled", () => {
    mocks.isFeatureEnabled.mockReturnValue(true);
    renderWizard({ toolset: oauthToolset });
    expect(
      screen.getByText(
        "Automatically set up OAuth Proxy based on pre-discovered details about this MCP server.",
      ),
    ).toBeTruthy();
    expect(screen.queryByText("Interactive auth enabled")).toBeNull();
  });
});

describe("OAuthWizard - external OAuth sources", () => {
  it("shows an explicit metadata source choice without a slug field", () => {
    renderWizard();
    fireEvent.click(screen.getByRole("button", { name: /External OAuth/ }));

    expect(
      screen.getByRole("button", { name: /Provider-hosted metadata/ }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: /Gram-hosted metadata/ }),
    ).toBeTruthy();
    expect(screen.queryByLabelText("OAuth Server Slug")).toBeNull();
  });

  it("renders discovered provider metadata for review", async () => {
    mocks.fetchRemoteSessionIssuerMetadata.mockResolvedValueOnce({
      issuer: "https://auth.example.com",
      authorizationEndpoint: "https://auth.example.com/authorize",
      tokenEndpoint: "https://auth.example.com/token",
      authorizationResponseIssParameterSupported: true,
      clientIdMetadataDocumentSupported: false,
      discoveryWarnings: [
        'discovery issuer "https://other.example.com" does not match requested "https://auth.example.com"',
      ],
      oidc: false,
      passthrough: true,
    });
    renderWizard();
    fireEvent.click(screen.getByRole("button", { name: /External OAuth/ }));
    fireEvent.click(
      screen.getByRole("button", { name: /Provider-hosted metadata/ }),
    );
    fireEvent.change(screen.getByLabelText("Issuer URL"), {
      target: { value: "https://auth.example.com" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Verify metadata" }));

    await waitFor(() => {
      expect(screen.getByText("https://auth.example.com")).toBeTruthy();
    });
    expect(screen.getByText("https://auth.example.com/authorize")).toBeTruthy();
    expect(screen.getByText("https://auth.example.com/token")).toBeTruthy();
    expect(screen.getByText("Supported")).toBeTruthy();
    expect(
      screen.getByText(/discovery issuer .* does not match requested/),
    ).toBeTruthy();
  });

  it("associates issuer validation text with the issuer input", () => {
    renderWizard();
    fireEvent.click(screen.getByRole("button", { name: /External OAuth/ }));
    fireEvent.click(
      screen.getByRole("button", { name: /Provider-hosted metadata/ }),
    );

    const input = screen.getByLabelText("Issuer URL");
    fireEvent.change(input, { target: { value: "http://auth.example.com" } });
    const error = screen.getByText("Provider issuer must use HTTPS");

    expect(error.id).not.toBe("");
    expect(input.getAttribute("aria-describedby")?.split(" ")).toContain(
      error.id,
    );
  });

  it("warns that Gram-hosted metadata is for multi-origin compatibility", () => {
    renderWizard();
    fireEvent.click(screen.getByRole("button", { name: /External OAuth/ }));
    fireEvent.click(
      screen.getByRole("button", { name: /Gram-hosted metadata/ }),
    );

    expect(
      screen.getByText(
        "Gram hosts authorization-server metadata in this mode. Modern clients may reject multi-origin OAuth configurations without issuer-bound responses.",
      ),
    ).toBeTruthy();
    expect(screen.getByLabelText("OAuth Metadata JSON")).toBeTruthy();
  });
});

describe("OAuthWizard — existing external OAuth config", () => {
  const existingConfig = {
    issuer: "https://auth.example.com",
    metadata: { issuer: "https://auth.example.com" },
  };

  it("allows existing configurations to be managed on the base tier", () => {
    mocks.productTier = ["base"];
    renderWizard({ existingConfig });

    expect(screen.getByText("Review OAuth metadata")).toBeTruthy();
    expect(
      screen.queryByText("A Managed OAuth integration requires upgrading"),
    ).toBeNull();
  });

  it("does not discover until review and only updates after confirmation", async () => {
    mocks.fetchRemoteSessionIssuerMetadata.mockResolvedValueOnce({
      issuer: existingConfig.issuer,
      authorizationEndpoint: "https://auth.example.com/authorize",
      tokenEndpoint: "https://auth.example.com/token",
      authorizationResponseIssParameterSupported: true,
      discoveryWarnings: [
        'discovery issuer "https://other.example.com" does not match requested "https://auth.example.com"',
      ],
    });
    renderWizard({ existingConfig });

    expect(mocks.fetchRemoteSessionIssuerMetadata).not.toHaveBeenCalled();
    expect(mocks.updateExternalOAuth).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Review update" }));
    await screen.findByText(/auth\.example\.com\/authorize/);
    expect(mocks.fetchRemoteSessionIssuerMetadata).toHaveBeenCalledTimes(1);
    expect(screen.getByText("Supported")).toBeTruthy();
    expect(
      screen.getByText(/discovery issuer .* does not match requested/),
    ).toBeTruthy();
    expect(mocks.updateExternalOAuth).not.toHaveBeenCalled();

    fireEvent.click(
      screen.getByRole("button", { name: "Use provider-hosted metadata" }),
    );
    await waitFor(() =>
      expect(mocks.updateExternalOAuth).toHaveBeenCalledTimes(1),
    );
    expect(mocks.invalidateAllToolset).toHaveBeenCalled();
    expect(mocks.invalidateAllGetMcpMetadata).toHaveBeenCalled();
  });

  it("treats a missing RFC 9207 capability as unsupported", async () => {
    mocks.fetchRemoteSessionIssuerMetadata.mockResolvedValueOnce({
      issuer: existingConfig.issuer,
      authorizationEndpoint: "https://auth.example.com/authorize",
    });
    renderWizard({ existingConfig });

    fireEvent.click(screen.getByRole("button", { name: "Review update" }));

    const rfcStatus = (await screen.findByText("RFC 9207")).parentElement;
    expect(rfcStatus?.textContent).toContain("Unsupported");
    expect(rfcStatus?.textContent).not.toContain("Not advertised");
  });

  it("wraps long discovered endpoint values", async () => {
    const authorizationEndpoint = `https://auth.example.com/${"a".repeat(200)}`;
    mocks.fetchRemoteSessionIssuerMetadata.mockResolvedValueOnce({
      issuer: existingConfig.issuer,
      authorizationEndpoint,
    });
    renderWizard({ existingConfig });

    fireEvent.click(screen.getByRole("button", { name: "Review update" }));

    expect(
      (await screen.findByText(authorizationEndpoint)).className,
    ).toContain("break-all");
  });

  it("changes a provider-hosted issuer after reviewing the replacement", async () => {
    mocks.fetchRemoteSessionIssuerMetadata.mockResolvedValueOnce({
      issuer: "https://replacement.example.com",
      authorizationEndpoint: "https://replacement.example.com/authorize",
      authorizationResponseIssParameterSupported: false,
    });
    renderWizard({
      existingConfig: { ...existingConfig, providerHosted: true },
    });

    fireEvent.change(screen.getByLabelText("Issuer URL"), {
      target: { value: "https://replacement.example.com" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Review update" }));
    await screen.findByText(/replacement\.example\.com\/authorize/);
    expect(screen.getByText("Unsupported")).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: "Use provider-hosted metadata" }),
    );
    await waitFor(() => expect(mocks.updateExternalOAuth).toHaveBeenCalled());
    expect(mocks.updateExternalOAuth).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          updateExternalOAuthServerRequestBody: {
            authorizationServerIssuer: "https://replacement.example.com",
          },
        }),
      }),
    );
  });

  it("resets a Gram-hosted metadata draft after closing", () => {
    const rendered = renderWizard({ existingConfig });
    fireEvent.click(
      screen.getByRole("button", { name: "Keep Gram-hosted metadata" }),
    );
    fireEvent.change(screen.getByLabelText("OAuth Metadata JSON"), {
      target: { value: '{"issuer":"https://draft.example.com"}' },
    });

    const modal = (isOpen: boolean) => (
      <MemoryRouter>
        <QueryClientProvider client={new QueryClient()}>
          <ConnectOAuthModal
            isOpen={isOpen}
            onClose={() => {}}
            toolsetSlug="mytoolset"
            toolset={toolset}
            existingConfig={existingConfig}
          />
        </QueryClientProvider>
      </MemoryRouter>
    );
    rendered.rerender(modal(false));
    rendered.rerender(modal(true));
    fireEvent.click(
      screen.getByRole("button", { name: "Keep Gram-hosted metadata" }),
    );

    expect(
      (screen.getByLabelText("OAuth Metadata JSON") as HTMLTextAreaElement)
        .value,
    ).toBe(JSON.stringify(existingConfig.metadata, null, 2));
  });

  it("ignores discovery from a closed session after a rapid reopen", async () => {
    let resolveDiscovery!: (value: Record<string, unknown>) => void;
    mocks.fetchRemoteSessionIssuerMetadata.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveDiscovery = resolve;
      }),
    );
    const rendered = renderWizard({ existingConfig });

    fireEvent.click(screen.getByRole("button", { name: "Review update" }));
    const modal = (isOpen: boolean) => (
      <MemoryRouter>
        <QueryClientProvider client={new QueryClient()}>
          <ConnectOAuthModal
            isOpen={isOpen}
            onClose={() => {}}
            toolsetSlug="mytoolset"
            toolset={toolset}
            existingConfig={existingConfig}
          />
        </QueryClientProvider>
      </MemoryRouter>
    );
    rendered.rerender(modal(false));
    expect(
      mocks.fetchRemoteSessionIssuerMetadata.mock.calls[0]![0].options
        .fetchOptions.signal.aborted,
    ).toBe(true);
    rendered.rerender(modal(true));

    await act(async () => {
      resolveDiscovery({
        issuer: existingConfig.issuer,
        authorizationEndpoint: "https://stale.example.com/authorize",
      });
    });

    expect(screen.queryByText(/stale\.example\.com/)).toBeNull();
    expect(screen.getByRole("button", { name: "Review update" })).toBeTruthy();
  });

  it("cancels without discovery or mutation", () => {
    const onClose = vi.fn();
    renderWizard({ existingConfig, onClose: () => void onClose() });

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(mocks.fetchRemoteSessionIssuerMetadata).not.toHaveBeenCalled();
    expect(mocks.updateExternalOAuth).not.toHaveBeenCalled();
  });

  it("keeps discovery failures actionable without mutation", async () => {
    mocks.fetchRemoteSessionIssuerMetadata.mockRejectedValueOnce(
      new Error("provider unavailable"),
    );
    renderWizard({ existingConfig });
    fireEvent.click(screen.getByRole("button", { name: "Review update" }));

    expect(await screen.findByText("provider unavailable")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Review update" })).toBeTruthy();
    expect(mocks.updateExternalOAuth).not.toHaveBeenCalled();
  });

  it("confirms clearing with stored metadata and never discovers", async () => {
    renderWizard({ existingConfig });
    fireEvent.click(
      screen.getByRole("button", { name: "Keep Gram-hosted metadata" }),
    );
    expect(mocks.updateExternalOAuth).not.toHaveBeenCalled();

    fireEvent.click(
      screen.getByRole("button", { name: "Confirm Gram-hosted metadata" }),
    );
    await waitFor(() =>
      expect(mocks.updateExternalOAuth).toHaveBeenCalledTimes(1),
    );
    expect(mocks.updateExternalOAuth).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          updateExternalOAuthServerRequestBody: {
            metadata: existingConfig.metadata,
          },
        }),
      }),
    );
    expect(mocks.fetchRemoteSessionIssuerMetadata).not.toHaveBeenCalled();
  });

  it("clears a provider-hosted issuer without discovery", async () => {
    renderWizard({
      existingConfig: {
        issuer: existingConfig.issuer,
        providerHosted: true,
      },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Use Gram-hosted metadata" }),
    );
    fireEvent.change(screen.getByLabelText("OAuth Metadata JSON"), {
      target: { value: JSON.stringify(existingConfig.metadata) },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Confirm Gram-hosted metadata" }),
    );

    await waitFor(() => expect(mocks.updateExternalOAuth).toHaveBeenCalled());
    expect(mocks.updateExternalOAuth).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          updateExternalOAuthServerRequestBody: {
            metadata: existingConfig.metadata,
          },
        }),
      }),
    );
    expect(mocks.fetchRemoteSessionIssuerMetadata).not.toHaveBeenCalled();
  });

  it("preserves the confirmation after update failure", async () => {
    mocks.updateExternalOAuth.mockRejectedValueOnce(
      new Error("update rejected"),
    );
    renderWizard({ existingConfig });
    fireEvent.click(
      screen.getByRole("button", { name: "Keep Gram-hosted metadata" }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Confirm Gram-hosted metadata" }),
    );

    expect(await screen.findByText("update rejected")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Confirm Gram-hosted metadata" }),
    ).toBeTruthy();
  });
});

describe("OAuthWizard — happy proxy create", () => {
  it("walks path selection → metadata → credentials → success", async () => {
    const onClose = vi.fn();
    renderWizard({ onClose: () => void onClose() });

    fireEvent.click(screen.getByRole("button", { name: /OAuth Proxy/ }));

    fireEvent.change(screen.getByPlaceholderText("my-oauth-proxy"), {
      target: { value: "new-proxy" },
    });
    fireEvent.change(
      screen.getByPlaceholderText("https://provider.com/oauth/authorize"),
      { target: { value: "https://e.example/auth" } },
    );
    fireEvent.change(
      screen.getByPlaceholderText("https://provider.com/oauth/token"),
      { target: { value: "https://e.example/token" } },
    );
    fireEvent.change(screen.getByPlaceholderText("read, write, openid"), {
      target: { value: "read, write" },
    });

    fireEvent.click(screen.getByText("Next"));

    fireEvent.change(screen.getByPlaceholderText("your-client-id"), {
      target: { value: "cid" },
    });
    fireEvent.change(screen.getByPlaceholderText("your-client-secret"), {
      target: { value: "csec" },
    });

    fireEvent.click(screen.getByText("Configure OAuth Proxy"));

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Done" })).toBeTruthy();
    });

    expect(mocks.createUserSessionIssuer).toHaveBeenCalledTimes(1);
    expect(mocks.createRemoteSessionIssuer).toHaveBeenCalledTimes(1);
    expect(mocks.createRemoteSessionClient).toHaveBeenCalledTimes(1);
    expect(mocks.setToolsetUserSessionIssuer).toHaveBeenCalledTimes(1);
    expect(mocks.invalidateAllToolset).toHaveBeenCalled();
    expect(mocks.invalidateAllGetMcpMetadata).toHaveBeenCalled();
    expect(mocks.invalidateAllListEnvironments).toHaveBeenCalled();
    expect(mocks.capture).toHaveBeenCalledWith(
      "mcp_event",
      expect.objectContaining({ action: "oauth_proxy_configured" }),
    );

    fireEvent.click(screen.getByText("Done"));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

describe("OAuthWizard — provisioning failure", () => {
  it("surfaces the error and returns to the credentials step when provisioning fails", async () => {
    mocks.createRemoteSessionIssuer.mockRejectedValueOnce(
      new Error("upstream rejected"),
    );
    renderWizard();

    fireEvent.click(screen.getByRole("button", { name: /OAuth Proxy/ }));
    fireEvent.change(screen.getByPlaceholderText("my-oauth-proxy"), {
      target: { value: "new-proxy" },
    });
    fireEvent.change(
      screen.getByPlaceholderText("https://provider.com/oauth/authorize"),
      { target: { value: "https://e.example/auth" } },
    );
    fireEvent.change(
      screen.getByPlaceholderText("https://provider.com/oauth/token"),
      { target: { value: "https://e.example/token" } },
    );
    fireEvent.change(screen.getByPlaceholderText("read, write, openid"), {
      target: { value: "read" },
    });
    fireEvent.click(screen.getByText("Next"));
    fireEvent.change(screen.getByPlaceholderText("your-client-id"), {
      target: { value: "cid" },
    });
    fireEvent.change(screen.getByPlaceholderText("your-client-secret"), {
      target: { value: "csec" },
    });
    fireEvent.click(screen.getByText("Configure OAuth Proxy"));

    await waitFor(() => {
      expect(screen.getByText(/upstream rejected/i)).toBeTruthy();
    });

    expect(mocks.createUserSessionIssuer).toHaveBeenCalledTimes(1);
    expect(mocks.setToolsetUserSessionIssuer).not.toHaveBeenCalled();
  });
});
