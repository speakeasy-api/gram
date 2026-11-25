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
// The real control is a Radix select, which jsdom cannot drive; a native
// select with the same label and choices stands in for it.
vi.mock("./request-duration-select", () => ({
  RequestDurationSelect: ({
    value,
    onChange,
  }: {
    value: string;
    onChange: (value: string) => void;
  }) => (
    <label>
      How long do you need it?
      <select value={value} onChange={(e) => onChange(e.target.value)}>
        {["none", "1h", "2h", "4h", "eod"].map((key) => (
          <option key={key} value={key}>
            {key}
          </option>
        ))}
      </select>
    </label>
  ),
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

/** Opens the request dialog and sends it with the defaults. */
function requestThroughDialog(): void {
  fireEvent.click(screen.getByRole("button", { name: "Request Access" }));
  fireEvent.click(screen.getByRole("button", { name: "Send request" }));
}

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

    requestThroughDialog();
    expect(await screen.findByText("Request pending")).toBeTruthy();
  });

  it("asks in a dialog how long and why while requests are stored", async () => {
    accessRequests.on = true;
    accessRequests.grantable = new Set(["log:read"]);
    mutateAsync.mockResolvedValueOnce({
      sentToCount: 1,
      request: { id: "request-1" },
    });
    render(<RequestScopeAccess scopes={["log:read"]} resourceId="project-1" />);

    expect(screen.queryByLabelText("How long do you need it?")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Request Access" }));
    expect(await screen.findByRole("dialog")).toBeTruthy();
    expect(screen.getByLabelText("How long do you need it?")).toBeTruthy();
    fireEvent.change(screen.getByLabelText(/Why do you need it/), {
      target: { value: "  Checking a failed tool call  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send request" }));

    expect(await screen.findByText("Request pending")).toBeTruthy();
    // No preference is the default, so the request carries no window.
    expect(mutateAsync).toHaveBeenCalledWith({
      request: {
        requestAccessForm: {
          scope: "log:read",
          resourceId: "project-1",
          message: "Checking a failed tool call",
        },
      },
    });
  });

  it("sends the duration the requester picked", async () => {
    accessRequests.on = true;
    accessRequests.grantable = new Set(["log:read"]);
    mutateAsync.mockResolvedValueOnce({
      sentToCount: 1,
      request: { id: "request-1" },
    });
    render(<RequestScopeAccess scopes={["log:read"]} resourceId="project-1" />);

    fireEvent.click(screen.getByRole("button", { name: "Request Access" }));
    fireEvent.change(await screen.findByLabelText("How long do you need it?"), {
      target: { value: "4h" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send request" }));

    expect(await screen.findByText("Request pending")).toBeTruthy();
    expect(mutateAsync).toHaveBeenCalledWith({
      request: {
        requestAccessForm: {
          scope: "log:read",
          resourceId: "project-1",
          duration: "4h",
        },
      },
    });
  });

  it("asks straight away, without a dialog, while requests only send email", async () => {
    render(<RequestScopeAccess scopes={["log:read"]} resourceId="project-1" />);

    fireEvent.click(screen.getByRole("button", { name: "Request Access" }));
    expect(await screen.findByText("Request sent")).toBeTruthy();
    expect(screen.queryByRole("dialog")).toBeNull();
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
    requestThroughDialog();
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
    requestThroughDialog();
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
