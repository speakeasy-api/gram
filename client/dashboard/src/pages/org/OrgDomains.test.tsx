import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import OrgDomains from "./OrgDomains";
import type { NetworkIngressRolloutStatus } from "@/hooks/useNetworkIngressRollout";

const state = vi.hoisted(() => ({
  admin: false,
  rolloutStatus: "disabled" as NetworkIngressRolloutStatus,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => state.admin }),
}));
vi.mock("@/hooks/useNetworkIngressRollout", () => ({
  useNetworkIngressRollout: () => ({
    canManageIngress: state.admin,
    status: state.rolloutStatus,
    rolloutEnabled: state.rolloutStatus === "enabled",
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

it.each([
  { admin: false, status: "disabled", network: false },
  { admin: false, status: "enabled", network: true },
  { admin: false, status: "loading", network: true },
  { admin: false, status: "error", network: true },
  { admin: true, status: "disabled", network: true },
  { admin: true, status: "enabled", network: true },
  { admin: true, status: "loading", network: true },
  { admin: true, status: "error", network: true },
] as const)(
  "matches heading, description and tab to rollout knowledge (admin=$admin, status=$status)",
  ({ admin, status, network }) => {
    state.admin = admin;
    state.rolloutStatus = status;
    render(
      <QueryClientProvider client={new QueryClient()}>
        <OrgDomains />
      </QueryClientProvider>,
    );
    const title = network ? "Network Access" : "Custom Domain";
    expect(screen.getByRole("heading", { name: title })).toBeTruthy();
    expect(document.title).toBe(`${title} | Speakeasy`);
    expect(
      screen.getByText(
        network
          ? "Configure the public and private network surfaces used to reach your organization's hosted MCP servers."
          : "Connect a custom domain to serve your MCP servers from your own branded URL instead of the default platform domain.",
      ),
    ).toBeTruthy();
  },
);

it("updates non-admin page and browser titles as rollout knowledge changes", () => {
  state.admin = false;
  state.rolloutStatus = "loading";
  const client = new QueryClient();
  const page = (
    <QueryClientProvider client={client}>
      <OrgDomains />
    </QueryClientProvider>
  );
  const view = render(page);
  for (const status of ["disabled", "enabled", "error"] as const) {
    state.rolloutStatus = status;
    view.rerender(
      <QueryClientProvider client={client}>
        <OrgDomains />
      </QueryClientProvider>,
    );
    const title = status === "disabled" ? "Custom Domain" : "Network Access";
    expect(screen.getByRole("heading", { name: title })).toBeTruthy();
    expect(document.title).toBe(`${title} | Speakeasy`);
  }
});
