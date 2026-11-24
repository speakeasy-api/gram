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
  catalogueLoaded: true,
  grantable: new Set<string>(),
  updatedAt: 0,
  pending: [] as {
    scope: string;
    selector: { resourceId: string };
    status?: string;
    grantExpiresAt?: Date;
  }[],
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
        accessRequests.catalogueLoaded
          ? {
              label: slug,
              temporaryGrantable: accessRequests.grantable.has(slug),
            }
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
  // The pending list is read once, like the page's own query, so it still
  // shows a request an admin has since decided; the watcher's polled list of
  // every status sees the decision.
  useAccessRequests: (request?: { status?: string }) => ({
    data: {
      requests: accessRequests.pending.map((item) => ({
        id: "request-1",
        ...item,
        status: request?.status ?? item.status ?? "pending",
      })),
    },
    dataUpdatedAt: accessRequests.updatedAt,
    refetch: () => Promise.resolve(),
  }),
  invalidateAllAccessRequests: () => Promise.resolve(),
}));
const toast = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
}));
vi.mock("sonner", () => ({ toast }));
const invalidateQueries = vi.hoisted(() => vi.fn(() => Promise.resolve()));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({ invalidateQueries }),
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
  accessRequests.catalogueLoaded = true;
  accessRequests.grantable = new Set();
  accessRequests.pending = [];
  accessRequests.updatedAt = 0;
  mutateAsync.mockClear();
  toast.success.mockClear();
  toast.error.mockClear();
  toast.info.mockClear();
  invalidateQueries.mockClear();
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

  it("asks how long the access is needed while requests are stored", async () => {
    accessRequests.on = true;
    accessRequests.grantable = new Set(["log:read"]);
    mutateAsync.mockResolvedValueOnce({
      sentToCount: 1,
      request: { id: "request-1" },
    });
    render(<RequestScopeAccess scopes={["log:read"]} resourceId="project-1" />);

    expect(screen.getByLabelText("How long do you need it?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Request Access" }));
    expect(await screen.findByText("Request pending")).toBeTruthy();
    // No preference is the default, so the request carries no window.
    expect(mutateAsync).toHaveBeenCalledWith({
      request: {
        requestAccessForm: { scope: "log:read", resourceId: "project-1" },
      },
    });
  });

  it("does not ask how long while requests only send email", () => {
    render(<RequestScopeAccess scopes={["log:read"]} resourceId="project-1" />);

    expect(screen.queryByLabelText("How long do you need it?")).toBeNull();
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

  it("offers the request when the catalogue cannot be read", () => {
    accessRequests.on = true;
    accessRequests.catalogueLoaded = false;
    render(<RequestScopeAccess scopes={["org:read"]} />);

    expect(screen.getByRole("button", { name: "Request Access" })).toBeTruthy();
  });

  it("offers the request again when it was decided before the list saw it pending", async () => {
    accessRequests.on = true;
    accessRequests.grantable = new Set(["log:read"]);
    mutateAsync.mockResolvedValueOnce({
      sentToCount: 1,
      request: { id: "request-1" },
    });
    const { rerender } = render(
      <RequestScopeAccess scopes={["log:read"]} resourceId="project-1" />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Request Access" }));
    expect(await screen.findByText("Request pending")).toBeTruthy();

    // The first list read after the request no longer holds it.
    accessRequests.updatedAt = Date.now() + 1_000;
    rerender(
      <RequestScopeAccess scopes={["log:read"]} resourceId="project-1" />,
    );

    expect(
      await screen.findByRole("button", { name: "Request Access" }),
    ).toBeTruthy();
  });

  it("offers the request again once the pending request is decided", async () => {
    accessRequests.on = true;
    accessRequests.grantable = new Set(["log:read"]);
    mutateAsync.mockResolvedValueOnce({
      sentToCount: 1,
      request: { id: "request-1" },
    });
    const { rerender } = render(
      <RequestScopeAccess scopes={["log:read"]} resourceId="project-1" />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Request Access" }));
    expect(await screen.findByText("Request pending")).toBeTruthy();

    // The pending list picks the request up, then drops it once an admin
    // decides it and the app-wide watcher refreshes the list.
    accessRequests.pending = [
      { scope: "log:read", selector: { resourceId: "project-1" } },
    ];
    accessRequests.updatedAt = Date.now() + 1_000;
    rerender(
      <RequestScopeAccess scopes={["log:read"]} resourceId="project-1" />,
    );
    expect(screen.getByText("Request pending")).toBeTruthy();
    accessRequests.pending = [];
    accessRequests.updatedAt = Date.now() + 2_000;
    rerender(
      <RequestScopeAccess scopes={["log:read"]} resourceId="project-1" />,
    );

    expect(
      await screen.findByRole("button", { name: "Request Access" }),
    ).toBeTruthy();
  });
});
