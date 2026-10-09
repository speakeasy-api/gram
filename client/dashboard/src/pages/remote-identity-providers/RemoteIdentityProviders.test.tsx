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
  useRoutes: (overrides?: { projectSlug?: string }) => ({
    remoteIdentityProviders: {
      issuerDetail: {
        href: (id: string) =>
          `/example/projects/${overrides?.projectSlug ?? "active"}/remote-identity-providers/${id}`,
      },
    },
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    projects: [{ id: "example-project", slug: "owning" }],
  }),
  useProject: () => ({ id: "active-project", slug: "active" }),
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
const tierMocks = vi.hoisted(() => ({
  platformHasMore: vi.fn(() => false),
  fetchNextPage: vi.fn(),
}));
vi.mock("@gram/client/react-query/organizationRemoteSessionIssuers.js", () => {
  const listed = [
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
  ];

  const tierOf = (issuer: {
    projectId: string | null;
    organizationId: string | null;
  }) => {
    if (issuer.projectId) return "project";
    return issuer.organizationId ? "organization" : "platform";
  };
  return {
    invalidateAllOrganizationRemoteSessionIssuers: vi.fn(),
    // Stands in for the server's tier filter: each table lists its own tier.
    useOrganizationRemoteSessionIssuersInfinite: (request: {
      tier: string;
    }) => ({
      data: {
        pages: [
          {
            result: {
              items: listed.filter(
                (item) => tierOf(item.issuer) === request.tier,
              ),
            },
          },
        ],
      },
      isLoading: false,
      isError: false,
      hasNextPage: request.tier === "platform" && tierMocks.platformHasMore(),
      isFetchingNextPage: false,
      fetchNextPage: () => tierMocks.fetchNextPage(request.tier),
    }),
  };
});
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
  expect(screen.getByText("Platform Authorization Servers")).toBeTruthy();
  for (const [name, slug, id] of [
    ["Platform Example", "active", "platform-provider"],
    ["Organization Example", "active", "org-provider"],
    ["Project Example", "owning", "project-provider"],
  ]) {
    expect(
      screen
        .getByRole("link", { name: `View authorization server ${name}` })
        .getAttribute("href"),
    ).toBe(`/example/projects/${slug}/remote-identity-providers/${id}`);
  }
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
    screen.getByRole("button", { name: "New Authorization Server" }),
  );
  expect(screen.getByText("Create tenant provider")).toBeTruthy();
});

it("pages the platform catalog on its own without crowding the org's tables", () => {
  tierMocks.platformHasMore.mockReturnValue(true);
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <RemoteIdentityProvidersPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );

  // The org's own providers render from their own tier queries, whatever the
  // size of the catalog; only the platform table offers more.
  expect(screen.getByText("Organization Example")).toBeTruthy();
  expect(screen.getByText("Project Example")).toBeTruthy();
  const loadMore = screen.getAllByRole("button", { name: "Load more" });
  expect(loadMore).toHaveLength(1);
  fireEvent.click(loadMore[0] as HTMLElement);
  expect(tierMocks.fetchNextPage).toHaveBeenCalledWith("platform");
});
