import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import OrgDomains from "./OrgDomains";

const state = vi.hoisted(() => ({ admin: false }));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => state.admin }),
}));
vi.mock("@/hooks/useNetworkIngressRollout", () => ({
  useNetworkIngressRollout: () => ({
    canManageIngress: state.admin,
    status: "disabled",
    rolloutEnabled: false,
  }),
}));
vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => "enterprise",
}));
vi.mock("@/hooks/useToolsetUrl", () => ({
  useCustomDomain: () => ({ domain: undefined, refetch: vi.fn() }),
}));
vi.mock("@/hooks/useRootMcpEndpoint", () => ({
  useRootMcpEndpointMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/rootMcpServers", () => ({
  useRootMcpServers: () => ({ data: undefined }),
}));
vi.mock("@gram/client/react-query/customDomainMcpEndpoints", () => ({
  useCustomDomainMcpEndpoints: () => ({ data: undefined }),
}));
vi.mock("@gram/client/react-query/registerDomain", () => ({
  useRegisterDomainMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/deleteDomain", () => ({
  useDeleteDomainMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/updateDomain", () => ({
  useUpdateDomainMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/checkDomainHealth", () => ({
  useCheckDomainHealthMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("@/components/page-templates", async (importOriginal) => {
  const original = await importOriginal<object>();
  return {
    ...original,
    // Keep this test focused on the page chrome, not domain CRUD dialogs.
    SettingsPage: ({
      title,
      description,
    }: {
      title: string;
      description: string;
    }) => (
      <main>
        <h1>{title}</h1>
        <p>{description}</p>
      </main>
    ),
  };
});
afterEach(cleanup);

it.each([false, true])(
  "matches domains heading, description and tab to visible sections (admin=%s)",
  (admin) => {
    state.admin = admin;
    render(
      <QueryClientProvider client={new QueryClient()}>
        <OrgDomains />
      </QueryClientProvider>,
    );
    const title = admin ? "Network Access" : "Custom Domain";
    expect(screen.getByRole("heading", { name: title })).toBeTruthy();
    expect(document.title).toBe(`${title} | Speakeasy`);
    expect(
      screen.getByText(
        admin
          ? "Configure the public and private network surfaces used to reach your organization's hosted MCP servers."
          : "Connect a custom domain to serve your MCP servers from your own branded URL instead of the default platform domain.",
      ),
    ).toBeTruthy();
  },
);
