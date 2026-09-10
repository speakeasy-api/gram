import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { RemoteIdentityProvidersPage } from "./RemoteIdentityProviders";

vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    remoteIdentityProviders: {
      issuerDetail: {
        href: (id: string) => `/example/remote-identity-providers/${id}`,
      },
    },
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/page-templates", () => ({
  ResourceListPage: ({
    children,
    primaryAction,
  }: {
    children: ReactNode;
    primaryAction: ReactNode;
  }) => (
    <>
      {primaryAction}
      {children}
    </>
  ),
}));
vi.mock("@/components/ui/Dropdown", () => {
  const Part = ({ children }: { children?: ReactNode }) => <>{children}</>;
  return {
    DropdownMenu: Part,
    DropdownMenuContent: Part,
    DropdownMenuTrigger: Part,
    DropdownMenuSeparator: () => null,
    DropdownMenuItem: ({
      children,
      onClick,
    }: {
      children: ReactNode;
      onClick: () => void;
    }) => <button onClick={onClick}>{children}</button>,
  };
});
vi.mock("@gram/client/react-query/organizationRemoteSessionIssuers.js", () => ({
  useOrganizationRemoteSessionIssuers: () => ({
    data: {
      result: {
        items: [
          {
            issuer: {
              id: "platform-provider",
              name: "Platform Example",
              issuer: "https://platform.example.com",
              organizationId: null,
              projectId: null,
            },
            clientCount: 0,
          },
          {
            issuer: {
              id: "org-provider",
              name: "Organization Example",
              issuer: "https://org.example.com",
              organizationId: "example-org",
              projectId: null,
            },
            clientCount: 1,
          },
          {
            issuer: {
              id: "project-provider",
              name: "Project Example",
              issuer: "https://project.example.com",
              organizationId: "example-org",
              projectId: "example-project",
            },
            projectName: "Example Project",
            clientCount: 0,
          },
        ],
      },
    },
    isLoading: false,
  }),
}));
vi.mock(
  "@gram/client/react-query/moveOrganizationRemoteSessionIssuer.js",
  () => ({
    useMoveOrganizationRemoteSessionIssuerMutation: () => ({ mutate: vi.fn() }),
  }),
);
vi.mock(
  "@gram/client/react-query/refreshOrganizationRemoteSessionIssuerMetadata.js",
  () => ({
    useRefreshOrganizationRemoteSessionIssuerMetadataMutation: () => ({
      mutate: vi.fn(),
    }),
  }),
);
vi.mock("./CreateRemoteIdentityProviderSheet", () => ({
  CreateRemoteIdentityProviderSheet: ({ open }: { open: boolean }) =>
    open ? <div>Create tenant provider</div> : null,
}));
vi.mock("./CreateRemoteSessionClientSheet", () => ({
  CreateRemoteSessionClientSheet: ({
    issuer,
  }: {
    issuer: { name: string };
  }) => <div>Add client to {issuer.name}</div>,
}));

afterEach(cleanup);
// Platform management is absent unconditionally; RequireScope is stubbed above,
// so tenant actions here do not test org:admin authorization.
it("preserves tenant actions and read-only platform browsing without platform management", () => {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <RemoteIdentityProvidersPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  expect(screen.queryByText("Manage Platform Providers")).toBeNull();
  expect(screen.getByText("Platform Remote Identity Providers")).toBeTruthy();
  for (const name of ["Organization Example", "Project Example"]) {
    const row = screen.getByText(name).closest("tr");
    expect(row).not.toBeNull();
    expect(within(row!).getByRole("button", { name: "Delete" })).toBeTruthy();
  }
  const platformRow = screen.getByText("Platform Example").closest("tr");
  expect(platformRow).not.toBeNull();
  expect(
    within(platformRow!).queryByRole("button", { name: "Delete" }),
  ).toBeNull();
  fireEvent.click(
    within(platformRow!).getByRole("button", { name: "Add Client" }),
  );
  expect(screen.getByText("Add client to Platform Example")).toBeTruthy();
  fireEvent.click(
    screen.getByRole("button", { name: "New Remote Identity Provider" }),
  );
  expect(screen.getByText("Create tenant provider")).toBeTruthy();
});
