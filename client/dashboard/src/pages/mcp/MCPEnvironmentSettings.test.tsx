import { TooltipProvider } from "@/components/ui/Tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/components/sources/sources-hooks", () => ({
  useExternalMcpOAuthConfigStatus: () => "configured",
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ activeOrganizationId: "org-1" }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: vi.fn() }),
}));
vi.mock("@/hooks/useMissingEnvironmentVariables", () => ({
  useMissingRequiredEnvVars: () => 0,
}));
vi.mock("@/hooks/useToolsetUrl", () => ({
  useMcpUrl: () => ({ url: "https://gram.example/mcp/server" }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    environments: {
      Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
    },
  }),
}));
vi.mock("@gram/client/react-query/createEnvironment.js", () => ({
  useCreateEnvironmentMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock("@gram/client/react-query/getMcpMetadata.js", () => ({
  useGetMcpMetadata: () => ({ data: {} }),
  invalidateAllGetMcpMetadata: vi.fn(),
}));
vi.mock("@gram/client/react-query/listEnvironments.js", () => ({
  useListEnvironments: () => ({ data: { environments: [] } }),
  invalidateAllListEnvironments: vi.fn(),
}));
vi.mock("@gram/client/react-query/mcpMetadataSet.js", () => ({
  useMcpMetadataSetMutation: () => ({
    mutate: vi.fn(),
    mutateAsync: vi.fn(),
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/remoteSessionIssuers.js", () => ({
  useRemoteSessionIssuers: () => ({ data: { result: { items: [] } } }),
}));
vi.mock("@gram/client/react-query/toolset.js", () => ({
  invalidateAllToolset: vi.fn(),
}));
vi.mock("@gram/client/react-query/updateEnvironment.js", () => ({
  useUpdateEnvironmentMutation: () => ({
    mutate: vi.fn(),
    mutateAsync: vi.fn(),
    isPending: false,
  }),
}));
vi.mock("./useEnvironmentVariables", () => ({
  useEnvironmentVariables: () => [],
}));
vi.mock("./MCPDetails", () => ({
  PageSection: ({
    children,
    action,
  }: {
    children: ReactNode;
    action?: ReactNode;
  }) => (
    <section>
      {action}
      {children}
    </section>
  ),
  OAuthDetailsModal: ({
    isOpen,
    onManageMetadata,
  }: {
    isOpen: boolean;
    onManageMetadata?: () => void;
  }) =>
    isOpen ? (
      <button onClick={onManageMetadata}>Manage metadata source</button>
    ) : null,
  ConnectOAuthModal: ({
    isOpen,
    existingConfig,
  }: {
    isOpen: boolean;
    existingConfig?: { issuer: string };
  }) =>
    isOpen ? (
      <div data-testid="oauth-review-dialog">{existingConfig?.issuer}</div>
    ) : null,
}));
vi.mock("./x/tabs/settings/sections/authentication/authTarget", () => ({
  useToolsetAuthTarget: () => ({}),
}));
vi.mock(
  "./x/tabs/settings/sections/authentication/AttachRemoteIdentityProviderSheet",
  () => ({
    AttachRemoteIdentityProviderSheet: () => null,
  }),
);

import { MCPAuthenticationTab } from "./MCPEnvironmentSettings";

afterEach(cleanup);

describe("MCPAuthenticationTab external OAuth metadata recommendation", () => {
  it("renders the eligible recommendation and opens its review entry point", () => {
    const queryClient = new QueryClient();
    const toolset = {
      slug: "server",
      mcpSlug: "server",
      mcpEnabled: true,
      mcpIsPublic: true,
      oauthEnablementMetadata: { oauth2SecurityCount: 1 },
      externalOauthServer: {
        metadata: { issuer: "https://auth.example.com" },
      },
    } as unknown as Parameters<typeof MCPAuthenticationTab>[0]["toolset"];

    render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <MCPAuthenticationTab toolset={toolset} />
        </TooltipProvider>
      </QueryClientProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Review update" }));

    expect(screen.getByTestId("oauth-review-dialog").textContent).toBe(
      "https://auth.example.com",
    );
  });

  it("keeps management available for an existing config that is no longer eligible", () => {
    const queryClient = new QueryClient();
    const toolset = {
      slug: "server",
      mcpSlug: "server",
      mcpEnabled: true,
      mcpIsPublic: true,
      oauthEnablementMetadata: { oauth2SecurityCount: 0 },
      externalOauthServer: {
        authorizationServerIssuer: "https://current.example.com",
      },
    } as unknown as Parameters<typeof MCPAuthenticationTab>[0]["toolset"];

    render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <MCPAuthenticationTab toolset={toolset} />
        </TooltipProvider>
      </QueryClientProvider>,
    );

    expect(screen.getByRole("button", { name: "Manage" })).toBeTruthy();
  });

  it("opens metadata management for an existing provider-hosted config", () => {
    const queryClient = new QueryClient();
    const toolset = {
      slug: "server",
      mcpSlug: "server",
      mcpEnabled: true,
      mcpIsPublic: true,
      oauthEnablementMetadata: { oauth2SecurityCount: 1 },
      externalOauthServer: {
        authorizationServerIssuer: "https://current.example.com",
      },
    } as unknown as Parameters<typeof MCPAuthenticationTab>[0]["toolset"];

    render(
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <MCPAuthenticationTab toolset={toolset} />
        </TooltipProvider>
      </QueryClientProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Manage" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Manage metadata source" }),
    );

    expect(screen.getByTestId("oauth-review-dialog").textContent).toBe(
      "https://current.example.com",
    );
  });
});
