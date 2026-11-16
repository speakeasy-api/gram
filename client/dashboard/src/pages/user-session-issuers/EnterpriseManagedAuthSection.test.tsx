import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
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
vi.mock("@gram/client/react-query/organizationRemoteSessionIssuer.js", () => ({
  useOrganizationRemoteSessionIssuer: () => ({
    data: { name: "Okta", issuer: "https://acme.okta.com" },
  }),
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

  it("asks a project-specific provider to become organization-wide first", () => {
    show({ ...base, slug: "slack", projectId: "project-1" });
    expect(screen.queryByRole("button", { name: "Set up EMA" })).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: "Make organization-wide" }),
    );
    expect(screen.getByRole("dialog")).toBeDefined();
  });
});
