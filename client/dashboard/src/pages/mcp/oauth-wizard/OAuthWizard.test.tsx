import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
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
    addOAuthProxy: vi.fn().mockResolvedValue(undefined),
    createEnvironment: vi
      .fn()
      .mockResolvedValue({ slug: "env-new", name: "Toolset OAuth" }),
    deleteEnvironment: vi.fn().mockResolvedValue(undefined),
    updateOAuthProxy: vi.fn().mockResolvedValue(undefined),
    capture: vi.fn(),
    invalidateAllToolset: vi.fn(),
    invalidateAllGetMcpMetadata: vi.fn(),
    invalidateAllListEnvironments: vi.fn(),
  };
});

vi.mock("@gram/client/react-query", () => ({
  useGramContext: () => ({}),
  useListEnvironments: () => ({ data: { environments: [] } }),
  invalidateAllToolset: mocks.invalidateAllToolset,
  invalidateAllGetMcpMetadata: mocks.invalidateAllGetMcpMetadata,
  invalidateAllListEnvironments: mocks.invalidateAllListEnvironments,
  buildAddExternalOAuthServerMutation: () => ({
    mutationKey: [],
    mutationFn: mocks.addExternalOAuth,
  }),
  buildAddOAuthProxyServerMutation: () => ({
    mutationKey: [],
    mutationFn: mocks.addOAuthProxy,
  }),
  buildCreateEnvironmentMutation: () => ({
    mutationKey: [],
    mutationFn: mocks.createEnvironment,
  }),
  buildDeleteEnvironmentMutation: () => ({
    mutationKey: [],
    mutationFn: mocks.deleteEnvironment,
  }),
  buildUpdateOAuthProxyServerMutation: () => ({
    mutationKey: [],
    mutationFn: mocks.updateOAuthProxy,
  }),
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ activeOrganizationId: "org-1" }),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: mocks.capture }),
}));

vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => ["pro"],
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
  oauthProxyServer: undefined,
  oauthEnablementMetadata: { oauth2SecurityCount: 0 },
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

  it("renders the proxy edit form when editMode is supplied", () => {
    renderWizard({
      editMode: {
        proxyServer: {
          slug: "existing-proxy",
          audience: "aud-1",
          oauthProxyProviders: [
            {
              authorizationEndpoint: "https://e.example/auth",
              tokenEndpoint: "https://e.example/token",
              scopesSupported: ["read", "write"],
              tokenEndpointAuthMethodsSupported: ["client_secret_post"],
              environmentSlug: "env-existing",
            },
          ],
        },
      } as unknown as Parameters<typeof ConnectOAuthModal>[0]["editMode"],
    });
    expect(screen.getByText("Edit OAuth Proxy")).toBeTruthy();
    expect(
      (screen.getByPlaceholderText("my-oauth-proxy") as HTMLInputElement).value,
    ).toBe("existing-proxy");
  });
});

describe("OAuthWizard — happy proxy create", () => {
  it("walks path selection → metadata → credentials → success", async () => {
    const onClose = vi.fn();
    renderWizard({ onClose });

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

    expect(mocks.createEnvironment).toHaveBeenCalledTimes(1);
    expect(mocks.addOAuthProxy).toHaveBeenCalledTimes(1);
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

describe("OAuthWizard — partial-failure rollback", () => {
  it("invokes deleteEnvironment when addOAuthProxy fails and surfaces error", async () => {
    mocks.addOAuthProxy.mockRejectedValueOnce(new Error("upstream rejected"));
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

    expect(mocks.createEnvironment).toHaveBeenCalledTimes(1);
    expect(mocks.addOAuthProxy).toHaveBeenCalledTimes(1);
    expect(mocks.deleteEnvironment).toHaveBeenCalledTimes(1);
    expect(mocks.deleteEnvironment).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({ slug: "env-new" }),
      }),
    );
  });
});
