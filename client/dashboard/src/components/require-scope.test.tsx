import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

const mutateAsync = vi.hoisted(() =>
  vi.fn((): Promise<{ sentToCount: number; request?: { id: string } }> =>
    Promise.resolve({ sentToCount: 1 }),
  ),
);
// The access requests rollout, the scopes the catalogue says can be given
// temporarily, and the viewer's own pending requests.
const accessRequests = vi.hoisted(() => ({
  on: false,
  grantable: new Set<string>(),
  pending: [] as { scope: string; selector: { resourceId: string } }[],
}));

vi.mock("@/components/page-header", () => {
  const Header = ({ children }: { children: ReactNode }) => (
    <header data-testid="page-header">{children}</header>
  );
  return {
    PageHeader: Object.assign(Header, { Breadcrumbs: () => null }),
  };
});
vi.mock("@/routes.tsx", () => ({ useRoutes: () => ({}) }));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    isLoading: false,
    hasScope: () => false,
    hasAnyScope: () => false,
    hasAllScopes: () => false,
  }),
}));
vi.mock("@/hooks/useScopeCatalogue", () => ({
  useScopeCatalogue: () => ({
    catalogue: {
      get: (slug: string) =>
        accessRequests.grantable.has(slug)
          ? { label: slug, temporaryGrantable: true }
          : undefined,
    },
    tasks: [],
  }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({
    status: accessRequests.on ? "enabled" : "disabled",
  }),
}));
vi.mock("@gram/client/react-query/accessRequests.js", () => ({
  useAccessRequests: () => ({
    data: { requests: accessRequests.pending },
    refetch: () => Promise.resolve(),
  }),
}));
vi.mock("@gram/client/react-query/requestAccess.js", () => ({
  useRequestAccessMutation: () => ({ mutateAsync, isPending: false }),
}));
vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: () => ({ data: undefined }),
}));

import { Page } from "./page-layout";
import { RequestScopeAccess, RequireScope } from "./require-scope";

afterEach(() => {
  cleanup();
  accessRequests.on = false;
  accessRequests.grantable = new Set();
  accessRequests.pending = [];
  mutateAsync.mockClear();
});

describe("RequireScope page level", () => {
  it("keeps the page header when the gate sits outside a Page", () => {
    render(
      <RequireScope scope="org:admin" level="page">
        <p>secret</p>
      </RequireScope>,
    );

    expect(screen.getByText("Access restricted")).toBeTruthy();
    expect(screen.getAllByTestId("page-header")).toHaveLength(1);
    expect(screen.queryByText("secret")).toBeNull();
  });

  it("adds no second header inside a Page", () => {
    render(
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <RequireScope scope="org:admin" level="page">
            <p>secret</p>
          </RequireScope>
        </Page.Body>
      </Page>,
    );

    expect(screen.getByText("Access restricted")).toBeTruthy();
    expect(screen.getAllByTestId("page-header")).toHaveLength(1);
  });
});

describe("RequestScopeAccess", () => {
  it("starts over when the resource it would ask for changes", async () => {
    const { rerender } = render(
      <RequestScopeAccess scopes={["log:read"]} resourceId="project-1" />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Request Access" }));
    expect(await screen.findByText("Request sent")).toBeTruthy();
    expect(mutateAsync).toHaveBeenCalledWith({
      request: {
        requestAccessForm: { scope: "log:read", resourceId: "project-1" },
      },
    });

    rerender(
      <RequestScopeAccess scopes={["log:read"]} resourceId="project-2" />,
    );

    expect(screen.getByRole("button", { name: "Request Access" })).toBeTruthy();
    expect(screen.queryByText("Request sent")).toBeNull();
  });

  it("shows a stored request as pending", async () => {
    accessRequests.on = true;
    accessRequests.grantable = new Set(["log:read"]);
    mutateAsync.mockResolvedValueOnce({
      sentToCount: 1,
      request: { id: "request-1" },
    });
    render(<RequestScopeAccess scopes={["log:read"]} resourceId="project-1" />);

    fireEvent.click(screen.getByRole("button", { name: "Request Access" }));
    expect(await screen.findByText("Request pending")).toBeTruthy();
  });

  it("shows a request made earlier as pending without asking again", () => {
    accessRequests.on = true;
    accessRequests.grantable = new Set(["log:read"]);
    accessRequests.pending = [
      { scope: "log:read", selector: { resourceId: "project-1" } },
    ];
    render(<RequestScopeAccess scopes={["log:read"]} resourceId="project-1" />);

    expect(screen.getByText("Request pending")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Request Access" })).toBeNull();
  });

  it("sends people to an admin for access that cannot be requested", () => {
    accessRequests.on = true;
    render(<RequestScopeAccess scopes={["org:admin"]} />);

    expect(screen.queryByRole("button", { name: "Request Access" })).toBeNull();
    expect(screen.getByText(/Ask an organization administrator/)).toBeTruthy();
  });
});
