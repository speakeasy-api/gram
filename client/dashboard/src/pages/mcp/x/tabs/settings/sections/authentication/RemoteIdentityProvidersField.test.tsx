import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RemoteIdentityProvidersField } from "./RemoteIdentityProvidersField";

const { hasAnyScope } = vi.hoisted(() => ({ hasAnyScope: vi.fn() }));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasAnyScope: (scopes: Scope[]) => hasAnyScope(scopes) as boolean,
    hasAllScopes: (scopes: Scope[]) => hasAnyScope(scopes) as boolean,
    isLoading: false,
  }),
}));

// The route helper resolves :orgSlug from the URL; stubbing it keeps the
// expected href readable without standing up the org route tree.
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    remoteIdentityProviders: {
      issuerDetail: {
        href: (id: string) => `/org-slug/remote-identity-providers/${id}`,
      },
    },
  }),
}));

function issuer(overrides: Partial<RemoteSessionIssuer> = {}) {
  return {
    id: "issuer-1",
    name: "Acme Identity",
    issuer: "https://auth.example.com",
    slug: "acme-identity",
    organizationId: "org-1",
    projectId: "project-1",
    clientIdMetadataDocumentSupported: false,
    oidc: true,
    passthrough: false,
    createdAt: new Date(0),
    updatedAt: new Date(0),
    ...overrides,
  } satisfies RemoteSessionIssuer;
}

function renderField(
  issuers: RemoteSessionIssuer[],
  handlers: {
    onEdit?: () => void;
    onDelete?: () => void;
    allowAdditionalProviders?: boolean;
  } = {},
) {
  return render(
    <MemoryRouter>
      <RemoteIdentityProvidersField
        associatedIssuers={issuers}
        isLoading={false}
        allowAdditionalProviders={handlers.allowAdditionalProviders ?? true}
        onAdd={vi.fn<() => void>()}
        onEdit={handlers.onEdit ?? vi.fn<() => void>()}
        onDelete={handlers.onDelete ?? vi.fn<() => void>()}
      />
    </MemoryRouter>,
  );
}

describe("RemoteIdentityProvidersField", () => {
  beforeEach(() => {
    hasAnyScope.mockReturnValue(true);
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  // The gateway (meta MCP) case attaches one provider per member vendor, so
  // the add action must survive past the first attachment.
  it("keeps Attach Provider available once providers exist", () => {
    renderField([issuer()]);

    expect(
      screen.getByRole("button", { name: /attach provider/i }),
    ).toBeTruthy();
  });

  // Remote/tunneled servers have exactly one upstream: once it is attached,
  // no further attach may be offered.
  it("hides Attach Provider for single-upstream targets once a provider exists", () => {
    renderField([issuer()], { allowAdditionalProviders: false });

    expect(
      screen.queryByRole("button", { name: /attach provider/i }),
    ).toBeNull();
  });

  it("links the provider name to its detail page", () => {
    renderField([issuer()]);

    expect(
      screen.getByRole("link", { name: "Acme Identity" }).getAttribute("href"),
    ).toBe("/org-slug/remote-identity-providers/issuer-1");
  });

  // Inherited organization and platform providers resolve through the same
  // tenant-scoped detail page, which renders them read-only.
  it.each([
    ["organization", { projectId: "" }],
    ["platform", { projectId: "", organizationId: "" }],
  ])("links an inherited %s provider to the same route", (_tier, overrides) => {
    renderField([issuer(overrides)]);

    expect(
      screen.getByRole("link", { name: "Acme Identity" }).getAttribute("href"),
    ).toBe("/org-slug/remote-identity-providers/issuer-1");
  });

  // The detail page requires org:read/org:admin, so without them the name has
  // to stay plain text rather than dead-end on an Unauthorized page.
  it("renders the name unlinked without organization read scopes", () => {
    hasAnyScope.mockImplementation(
      (scopes: Scope[]) => !scopes.includes("org:read"),
    );

    renderField([issuer()]);

    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.getByText("Acme Identity")).toBeTruthy();
  });

  it("keeps Edit and Delete clickable beside the link", () => {
    const onEdit = vi.fn<() => void>();
    const onDelete = vi.fn<() => void>();
    renderField([issuer()], { onEdit, onDelete });

    fireEvent.click(screen.getByRole("button", { name: /edit/i }));
    fireEvent.click(screen.getByRole("button", { name: /delete/i }));

    expect(onEdit).toHaveBeenCalledTimes(1);
    expect(onDelete).toHaveBeenCalledTimes(1);
  });
});
