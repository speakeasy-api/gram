import type { ReactNode } from "react";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { UserSessionIssuersPage } from "./UserSessionIssuers";

const state = vi.hoisted(() => ({
  admin: true,
  rbacLoading: false,
  organizationIssuers: [] as UserSessionIssuer[],
  projectIssuers: [] as UserSessionIssuer[],
}));

vi.mock("./useIssuerTier", () => ({
  useIssuerTier: (tier: "organization" | "project") => ({
    items:
      tier === "organization"
        ? state.organizationIssuers
        : state.projectIssuers,
    isLoading: false,
    isError: false,
    isFetching: false,
    retry: vi.fn(),
  }),
  retryFailedTiers: () => undefined,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    isLoading: state.rbacLoading,
    hasScope: (scope: string) => scope === "org:admin" && state.admin,
  }),
}));
vi.mock("@/pages/org/identity-provider/tabs/setup/OktaSignInSection", () => ({
  OKTA_SIGN_IN_SECTION_ID: "okta-sign-in",
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    identity: { href: () => "/org/identity" },
    userSessionIssuers: {
      href: () => "/org/user-session-issuers",
      issuerDetail: { href: (id: string) => `/org/user-session-issuers/${id}` },
    },
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
    scope,
  }: {
    children: ReactNode;
    scope: string | string[];
  }) => (scope === "org:admin" && !state.admin ? null : <>{children}</>),
}));
vi.mock("@/components/page-templates", () => ({
  ResourceListPage: ({
    title,
    primaryAction,
    children,
  }: {
    title: string;
    primaryAction?: ReactNode;
    children: ReactNode;
  }) => (
    <main>
      <h1>{title}</h1>
      {primaryAction}
      {children}
    </main>
  ),
}));
vi.mock("./CreateOrganizationSignInSheet", () => ({
  CreateOrganizationSignInSheet: ({
    open,
    onOpenChange,
    oktaSetupHref,
    onCreated,
  }: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    oktaSetupHref: string;
    onCreated: (issuer: { id: string }) => void;
  }) =>
    open ? (
      <div role="dialog" aria-label="Create organization-wide sign-in">
        <a href={oktaSetupHref}>Okta setup</a>
        <button onClick={() => onOpenChange(false)}>Close sheet</button>
        <button
          onClick={() => {
            onOpenChange(false);
            onCreated({ id: "new-sign-in" });
          }}
        >
          Finish create
        </button>
      </div>
    ) : null,
}));

function Search(): JSX.Element {
  const location = useLocation();
  return (
    <>
      <output>{location.search}</output>
      <span data-testid="path">{location.pathname}</span>
    </>
  );
}

function show(path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route
          path="*"
          element={
            <>
              <UserSessionIssuersPage />
              <Search />
            </>
          }
        />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  state.admin = true;
  state.rbacLoading = false;
  state.organizationIssuers = [];
  state.projectIssuers = [];
});

afterEach(() => {
  cleanup();
});

describe("Create organization-wide sign-in", () => {
  it("opens from the page action", () => {
    show("/user-session-issuers");
    expect(screen.queryByRole("dialog")).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: "Create organization-wide sign-in" }),
    );
    expect(screen.getByRole("dialog")).toBeDefined();
    expect(screen.getByRole("status").textContent).toBe("?create=1");
  });

  it("opens straight away from ?create=1, and closing clears it", () => {
    show("/user-session-issuers?create=1");
    expect(screen.getByRole("dialog")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Close sheet" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("status").textContent).toBe("");
  });

  it("opens the new sign-in once it's created", () => {
    show("/user-session-issuers?create=1");
    fireEvent.click(screen.getByRole("button", { name: "Finish create" }));
    expect(screen.getByTestId("path").textContent).toBe(
      "/org/user-session-issuers/new-sign-in",
    );
  });

  it("links Okta sign-ins to the Okta sign-in setup", () => {
    show("/user-session-issuers?create=1");
    expect(
      screen.getByRole("link", { name: "Okta setup" }).getAttribute("href"),
    ).toBe(
      "/org/identity?tab=identity-providers&provider=okta&view=setup#okta-sign-in",
    );
  });

  it("is only for organization admins, and drops ?create=1 for others", () => {
    state.admin = false;
    show("/user-session-issuers?create=1");
    expect(
      screen.queryByRole("button", {
        name: "Create organization-wide sign-in",
      }),
    ).toBeNull();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("status").textContent).toBe("");
  });

  it("keeps ?create=1 until permissions have loaded", () => {
    state.rbacLoading = true;
    show("/user-session-issuers?create=1");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getByRole("status").textContent).toBe("?create=1");
  });
});

describe("Organization-wide sign-ins list", () => {
  it("calls them sign-ins throughout", () => {
    show("/user-session-issuers");
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(
      "Speakeasy sign-ins",
    );
    expect(screen.getByText("Project sign-ins")).toBeDefined();
    expect(screen.getByText("No project sign-ins")).toBeDefined();
    expect(screen.queryByText(/authorization server/i)).toBeNull();
  });

  it("names the section and its empty state plainly", () => {
    show("/user-session-issuers");
    expect(screen.getByText("Organization-wide sign-ins")).toBeDefined();
    expect(screen.getByText("Every project can use these.")).toBeDefined();
    expect(screen.getByText("No organization-wide sign-ins yet")).toBeDefined();
    expect(
      screen.getByText(
        "Create one to share a sign-in across projects. Enterprise Managed Authorization needs one.",
      ),
    ).toBeDefined();
  });

  it("no longer offers to make a project sign-in organization-wide", () => {
    state.projectIssuers = [
      {
        id: "p1",
        slug: "linear-abc",
        projectId: "proj-1",
        projectName: "Default",
      } as UserSessionIssuer,
    ];
    show("/user-session-issuers");
    fireEvent.keyDown(
      screen.getByRole("button", { name: "Actions for linear-abc" }),
      { key: "Enter" },
    );
    expect(screen.getByText("Consolidate into another")).toBeDefined();
    expect(screen.queryByText("Make organization-wide")).toBeNull();
  });
});
