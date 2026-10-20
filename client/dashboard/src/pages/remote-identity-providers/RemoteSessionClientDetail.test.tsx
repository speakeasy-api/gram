import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import RemoteSessionClientDetail from "./RemoteSessionClientDetail";

const BASE = "/rip/issuer-1/clients/client-1";

function Passthrough({ children }: { children?: ReactNode }) {
  return <>{children}</>;
}

vi.mock("@/components/page-layout", () => ({
  Page: Object.assign(Passthrough, {
    Header: Object.assign(Passthrough, { Breadcrumbs: () => null }),
    Body: Passthrough,
    Eyebrow: () => null,
  }),
}));
vi.mock("@/components/detail-hero", () => ({ DetailHero: Passthrough }));
vi.mock("@/components/require-scope", () => ({ RequireScope: Passthrough }));
vi.mock("@/lib/remote-identity", () => ({ ScopeBadge: () => null }));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
}));
vi.mock("@/routes", () => {
  const tab = (segment: string) => ({
    href: (issuerId: string, clientId: string) =>
      `/rip/${issuerId}/clients/${clientId}/${segment}`,
  });
  return {
    useRoutes: () => ({
      remoteIdentityProviders: {
        issuerDetail: { clients: { href: () => "/rip/issuer-1/clients" } },
        clientDetail: {
          overview: tab("overview"),
          mcpServers: tab("mcp-servers"),
          sessions: tab("sessions"),
        },
      },
    }),
  };
});
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));
vi.mock("@gram/client/react-query/organizationRemoteSessionClient.js", () => ({
  invalidateAllOrganizationRemoteSessionClient: vi.fn(),
  useOrganizationRemoteSessionClient: () => ({
    data: { id: "client-1", clientId: "upstream-client" },
    isLoading: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/organizationRemoteSessionIssuer.js", () => ({
  useOrganizationRemoteSessionIssuer: () => ({
    data: { id: "issuer-1", issuer: "https://idp.example.com" },
    isLoading: false,
  }),
}));
vi.mock(
  "@gram/client/react-query/updateOrganizationRemoteSessionClient.js",
  () => ({
    useUpdateOrganizationRemoteSessionClientMutation: () => ({
      mutate: vi.fn(),
      isPending: false,
    }),
  }),
);
vi.mock("./clientAlerts", () => ({ LegacyCallbackAlert: () => null }));
vi.mock("./tabs/client/OverviewTab", () => ({
  OverviewTab: () => <div>overview tab</div>,
}));
vi.mock("./tabs/client/McpServersTab", () => ({ McpServersTab: () => null }));
vi.mock("./tabs/client/SessionsTab", () => ({ SessionsTab: () => null }));

afterEach(cleanup);

function CurrentPath() {
  return <div data-testid="path">{useLocation().pathname}</div>;
}

function renderAt(path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route
          path="/rip/:issuerId/clients/:clientId/*"
          element={
            <>
              <RemoteSessionClientDetail />
              <CurrentPath />
            </>
          }
        />
      </Routes>
    </MemoryRouter>,
  );
}

describe("remote session client detail tabs", () => {
  // routes.test.tsx covers the route entry; this covers the redirect itself.
  it("redirects the retired settings tab to Overview", () => {
    renderAt(`${BASE}/settings`);

    expect(screen.getByTestId("path").textContent).toBe(`${BASE}/overview`);
    expect(screen.getByText("overview tab")).toBeTruthy();
  });

  it("offers Overview, MCP Servers and Sessions only", () => {
    renderAt(`${BASE}/overview`);

    const tabs = screen.getAllByRole("tab").map((tab) => tab.textContent);
    expect(tabs).toEqual(["Overview", "MCP Servers", "Sessions"]);
  });
});
