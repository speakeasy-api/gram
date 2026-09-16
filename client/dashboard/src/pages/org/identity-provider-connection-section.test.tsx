import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { IdentityProviderConnection } from "@gram/client/models/components/identityproviderconnection.js";
import { IdentityProviderConnectionSection } from "./identity-provider-connection-section";

const identityProvider = vi.hoisted(() => ({
  current: { data: { connection: undefined } } as {
    data: { connection: IdentityProviderConnection | undefined };
  },
}));

vi.mock("@gram/client/react-query/identityProvider.js", () => ({
  useIdentityProvider: () => identityProvider.current,
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    setupTask: {
      Link: ({ children }: { children: React.ReactNode }) => (
        <a href="/org/setup/idp">{children}</a>
      ),
    },
  }),
}));

// Placeholder tenant only — never a real customer's Okta hostname.
const TENANT = "example.okta.com";

function connection(
  overrides: Partial<IdentityProviderConnection> = {},
): IdentityProviderConnection {
  return {
    id: "conn-1",
    kind: "okta",
    tenantIdentifier: TENANT,
    status: "active",
    capabilities: ["directory_read"],
    grantedScopes: [],
    jwksUrl:
      "https://app.example.test/.well-known/identity-provider/abc/jwks.json",
    signingKeyKid: "kid-abc123",
    createdAt: new Date("2026-09-15T10:00:00Z"),
    updatedAt: new Date("2026-09-15T10:00:00Z"),
    ...overrides,
  };
}

afterEach(cleanup);
beforeEach(() => {
  identityProvider.current = { data: { connection: undefined } };
});

describe("IdentityProviderConnectionSection", () => {
  it("renders nothing until a connection exists", () => {
    const { container } = render(<IdentityProviderConnectionSection />);
    expect(container.firstChild).toBeNull();
  });

  it("shows the tenant, the key and what the last check proved", () => {
    identityProvider.current = {
      data: {
        connection: connection({
          signInState: "application_created",
          lastVerifiedAt: new Date("2026-09-15T12:00:00Z"),
          verifyEvidence: {
            checkedAt: new Date("2026-09-15T12:00:00Z"),
            reads: [
              {
                capability: "directory_read",
                resource: "groups",
                ok: true,
                count: 18,
              },
            ],
          },
        }),
      },
    };

    render(<IdentityProviderConnectionSection />);

    expect(screen.getByText(TENANT)).toBeTruthy();
    expect(screen.getByText("Connected")).toBeTruthy();
    expect(screen.getByText("People and group membership")).toBeTruthy();
    expect(screen.getByText("18 groups")).toBeTruthy();
    // Sign-on has not been checked, so there is still setup to go back to.
    expect(screen.getByText(/created in Okta/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "Continue setup" })).toBeTruthy();
  });

  it("offers a review rather than a continuation once sign-on has passed", () => {
    identityProvider.current = {
      data: { connection: connection({ signInState: "passed" }) },
    };

    render(<IdentityProviderConnectionSection />);

    expect(
      screen.getByText("Sign-on is configured and has been checked."),
    ).toBeTruthy();
    expect(screen.getByRole("link", { name: "Review setup" })).toBeTruthy();
    expect(screen.getByText("Not checked yet")).toBeTruthy();
  });

  it("names a status that is not yet connected without claiming it is", () => {
    identityProvider.current = {
      data: { connection: connection({ status: "failed" }) },
    };

    render(<IdentityProviderConnectionSection />);

    expect(screen.getByText("Last check did not pass")).toBeTruthy();
    expect(screen.queryByText("Connected")).toBeNull();
    expect(screen.getByRole("link", { name: "Continue setup" })).toBeTruthy();
  });

  it("is read only — it offers no action but the link back to setup", () => {
    identityProvider.current = {
      data: { connection: connection({ signInState: "passed" }) },
    };

    const { container } = render(<IdentityProviderConnectionSection />);

    expect(container.querySelectorAll("button")).toHaveLength(0);
    expect(container.querySelectorAll("input")).toHaveLength(0);
    expect(container.querySelectorAll("a")).toHaveLength(1);
  });
});
