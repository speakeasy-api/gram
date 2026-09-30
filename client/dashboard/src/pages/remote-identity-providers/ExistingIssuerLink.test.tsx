import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import type { RemoteSessionIssuerDuplicateMatch } from "@gram/client/models/components/remotesessionissuerduplicatematch.js";
import { ExistingIssuerLink } from "./ExistingIssuerLink";

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    projects: [{ id: "other-project", slug: "other" }],
  }),
  useProject: () => ({ id: "active-project", slug: "active" }),
}));
vi.mock("@/routes", () => ({
  useRoutes: ({ projectSlug }: { projectSlug: string }) => ({
    remoteIdentityProviders: {
      issuerDetail: {
        href: (id: string) =>
          `/org/projects/${projectSlug}/remote-identity-providers/${id}`,
      },
    },
  }),
}));
// Resolved by id, so a match past the org's first listing page still links.
const issuersById: Record<string, { id: string; projectId: string }> = {
  "other-issuer": { id: "other-issuer", projectId: "other-project" },
  "inaccessible-issuer": {
    id: "inaccessible-issuer",
    projectId: "inaccessible-project",
  },
};
vi.mock("@gram/client/react-query/organizationRemoteSessionIssuer.js", () => ({
  useOrganizationRemoteSessionIssuer: (
    request: { id: string },
    _security: unknown,
    options: { enabled: boolean },
  ) => ({ data: options.enabled ? issuersById[request.id] : undefined }),
}));
afterEach(cleanup);

const match: RemoteSessionIssuerDuplicateMatch = {
  id: "other-issuer",
  name: "Example",
  issuer: "https://example.com",
  slug: "example",
  projectName: "Other",
  tier: "project-specific",
};
it.each([
  ["project-specific", "other"],
  ["organization-level", "active"],
  ["platform-level", "active"],
] as const)(
  "opens a %s duplicate in the correct project",
  (tier, projectSlug) => {
    render(
      <MemoryRouter>
        <ExistingIssuerLink match={{ ...match, tier }} />
      </MemoryRouter>,
    );
    expect(
      screen
        .getByRole("link", { name: "View existing provider" })
        .getAttribute("href"),
    ).toBe(
      `/org/projects/${projectSlug}/remote-identity-providers/other-issuer`,
    );
  },
);
it.each(["missing", "inaccessible-issuer"])(
  "does not link unresolved provider %s into the active project",
  (id) => {
    render(
      <MemoryRouter>
        <ExistingIssuerLink match={{ ...match, id }} />
      </MemoryRouter>,
    );
    expect(screen.queryByRole("link")).toBeNull();
  },
);
