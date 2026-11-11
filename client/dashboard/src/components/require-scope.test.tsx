import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

const mutateAsync = vi.hoisted(() =>
  vi.fn(() => Promise.resolve({ sentToCount: 1 })),
);

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
  useScopeCatalogue: () => ({ catalogue: new Map(), tasks: [] }),
}));
vi.mock("@gram/client/react-query/requestAccess.js", () => ({
  useRequestAccessMutation: () => ({ mutateAsync, isPending: false }),
}));
vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: () => ({ data: undefined }),
}));

import { Page } from "./page-layout";
import { RequestScopeAccess, RequireScope } from "./require-scope";

afterEach(cleanup);

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
});
