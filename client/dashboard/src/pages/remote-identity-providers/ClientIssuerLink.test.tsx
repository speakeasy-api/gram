import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { ClientIssuerLink } from "./ClientIssuerLink";

const isPlatformAdmin = vi.fn();
const hasAnyScope = vi.fn();

vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => isPlatformAdmin(),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    remoteIdentityProviders: {
      issuerDetail: {
        href: (id: string) => `/org/projects/p/remote-identity-providers/${id}`,
      },
    },
  }),
}));

beforeEach(() => {
  isPlatformAdmin.mockReturnValue(false);
  hasAnyScope.mockReturnValue(true);
});
afterEach(cleanup);

const issuer = {
  id: "issuer-1",
  name: "Example",
  issuer: "https://example.com",
  projectId: "",
  organizationId: "",
} as RemoteSessionIssuer;

const renderLink = (overrides: Partial<RemoteSessionIssuer> = {}) =>
  render(
    <MemoryRouter>
      <ClientIssuerLink issuer={{ ...issuer, ...overrides }} />
    </MemoryRouter>,
  );

it("renders a platform provider as plain text for non-platform admins", () => {
  renderLink();
  expect(screen.queryByRole("link")).toBeNull();
  expect(screen.getByText("Example")).toBeTruthy();
});

it("links a platform provider for platform admins", () => {
  isPlatformAdmin.mockReturnValue(true);
  renderLink();
  expect(
    screen.getByRole("link", { name: "Example" }).getAttribute("href"),
  ).toBe("/org/projects/p/remote-identity-providers/issuer-1");
});

it.each([
  ["organizational", { organizationId: "org-1" }],
  ["project-specific", { organizationId: "org-1", projectId: "project-1" }],
])("links an %s provider for org members", (_tier, owners) => {
  renderLink(owners);
  expect(screen.getByRole("link", { name: "Example" })).toBeTruthy();
});

it("renders plain text without org scopes", () => {
  hasAnyScope.mockReturnValue(false);
  renderLink({ organizationId: "org-1" });
  expect(screen.queryByRole("link")).toBeNull();
});
