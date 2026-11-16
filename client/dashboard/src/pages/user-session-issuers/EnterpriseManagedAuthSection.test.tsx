import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { EnterpriseManagedAuthSection } from "./EnterpriseManagedAuthSection";

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
  }: {
    children: ReactNode | ((props: { disabled: boolean }) => ReactNode);
  }) => (
    <>
      {typeof children === "function"
        ? children({ disabled: false })
        : children}
    </>
  ),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ identity: { href: () => "/acme/identity" } }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
const okta = {
  isPending: false,
  isError: false,
  data: { id: "remote-1", name: "Okta", issuer: "https://acme.okta.com" },
};
const trusted = vi.hoisted(() => ({
  state: undefined as unknown,
}));
vi.mock("@gram/client/react-query/organizationRemoteSessionIssuer.js", () => ({
  useOrganizationRemoteSessionIssuer: () => trusted.state,
}));
vi.mock("@/lib/remote-identity/components/ProviderLink", () => ({
  IssuerLink: ({ issuer }: { issuer: { id: string; name: string } }) => (
    <a href={`/remote-identity-providers/${issuer.id}`}>{issuer.name}</a>
  ),
}));
vi.mock("./MakeOrganizationWideDialog", () => ({
  MakeOrganizationWideDialog: () => <div role="dialog">Make wide</div>,
}));

const base = {
  id: "usi-1",
  slug: "acme-workforce",
  projectId: "",
  organizationId: "org-1",
  issuerUrl: "https://app.example.com/oauth/usi/usi-1",
} as UserSessionIssuer;

function show(
  issuer: UserSessionIssuer,
  path = "/acme/mcp-authentication/usi-1",
) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={[path]}>
        <EnterpriseManagedAuthSection issuer={issuer} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  trusted.state = okta;
});
afterEach(cleanup);

describe("EnterpriseManagedAuthSection", () => {
  it("offers setup when off, with Speakeasy shown but not choosable", () => {
    show(base);
    expect(screen.getByText("Off")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Set up EMA" }));
    expect(
      screen.getByRole("radio", { name: /Speakeasy/ }).hasAttribute("disabled"),
    ).toBe(true);
    const okta = screen.getByRole("link", { name: "Continue with Okta" });
    const href = new URL(okta.getAttribute("href") ?? "", "https://x");
    expect(href.pathname).toBe("/acme/identity");
    expect(href.searchParams.get("provider")).toBe("okta");
    expect(href.searchParams.get("emaFor")).toBe("acme-workforce");
    expect(href.searchParams.get("emaReturn")).toBe(
      "/acme/mcp-authentication/usi-1",
    );
  });

  it("names the trusted provider and offers to turn it off", () => {
    show({ ...base, trustedRemoteSessionIssuerId: "remote-1" });
    expect(screen.getByText("On")).toBeDefined();
    expect(screen.getByText("Okta")).toBeDefined();
    expect(screen.getByRole("button", { name: "Turn off" })).toBeDefined();
  });

  it("sends changes to the Okta connection when browser sign-in uses the provider too", () => {
    show({
      ...base,
      trustedRemoteSessionIssuerId: "remote-1",
      trustedRemoteSessionClientId: "client-1",
    });
    expect(screen.queryByRole("button", { name: "Turn off" })).toBeNull();
    expect(
      screen
        .getByRole("link", { name: "Okta connection" })
        .getAttribute("href"),
    ).toContain("view=setup");
  });

  it("points a provider other than Okta at its own page, not Okta's", () => {
    trusted.state = {
      ...okta,
      data: {
        id: "remote-1",
        name: "Login",
        issuer: "https://login.example.com",
      },
    };
    show({
      ...base,
      trustedRemoteSessionIssuerId: "remote-1",
      trustedRemoteSessionClientId: "client-1",
    });
    expect(screen.queryByRole("link", { name: "Okta connection" })).toBeNull();
    expect(
      screen.getByRole("link", { name: "Login" }).getAttribute("href"),
    ).toBe("/remote-identity-providers/remote-1");
  });

  it("treats a provider Okta's own setup created as Okta, whatever its domain", () => {
    trusted.state = {
      ...okta,
      data: {
        id: "remote-1",
        slug: "okta-sign-in",
        name: "Login",
        issuer: "https://login.example.com",
      },
    };
    show({
      ...base,
      trustedRemoteSessionIssuerId: "remote-1",
      trustedRemoteSessionClientId: "client-1",
    });
    expect(screen.getByRole("link", { name: "Okta connection" })).toBeDefined();
  });

  it("says so when the trusted provider can't be loaded", () => {
    trusted.state = { isPending: false, isError: true, data: undefined };
    show({ ...base, trustedRemoteSessionIssuerId: "remote-1" });
    expect(screen.queryByText("Loading…")).toBeNull();
    expect(
      screen.getByText(/Couldn't load this identity provider/),
    ).toBeDefined();
  });

  it("won't swap the provider from a return link while browser sign-in uses it", () => {
    show(
      {
        ...base,
        trustedRemoteSessionIssuerId: "remote-1",
        trustedRemoteSessionClientId: "client-1",
      },
      "/acme/mcp-authentication/usi-1?ema=https%3A%2F%2Fother.example.com",
    );
    expect(screen.queryByRole("button", { name: "Turn on EMA" })).toBeNull();
    expect(screen.getByRole("link", { name: "Okta connection" })).toBeDefined();
  });

  it("asks a project-specific provider to become organization-wide first", () => {
    show({ ...base, slug: "slack", projectId: "project-1" });
    expect(screen.queryByRole("button", { name: "Set up EMA" })).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: "Make organization-wide" }),
    );
    expect(screen.getByRole("dialog")).toBeDefined();
  });
});
