import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// What the page sends when a row changes. The rules the surface writes are the
// whole point, so the tests assert the payload rather than the rendering alone.
const mutate = vi.fn();

vi.mock("@gram/client/react-query/setResourceAudience.js", () => ({
  useSetResourceAudienceMutation: () => ({ mutate, isPending: false }),
}));

vi.mock("@gram/client/react-query/resourceAudience.js", () => ({
  invalidateAllResourceAudience: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("react-router", () => ({
  useNavigate: () => vi.fn(),
}));

vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    access: { roles: { href: () => "/org/access/roles" } },
  }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

vi.mock("@/components/identity-link", () => ({
  IdentityLink: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

import { ManageAccess } from "./ManageAccess";

afterEach(cleanup);
beforeEach(() => mutate.mockClear());

function entry(
  overrides: Partial<ResourceAudienceEntry>,
): ResourceAudienceEntry {
  return {
    principalUrn: "user:1",
    kind: "user",
    displayName: "Hana Sato",
    level: "use",
    appliesTo: "resource",
    ...overrides,
  } as ResourceAudienceEntry;
}

function renderList(entries: ResourceAudienceEntry[]) {
  return render(
    <ManageAccess
      resourceId="server-1"
      resourceName="Acme Ops"
      entries={entries}
      version="v1"
      isLoading={false}
    />,
  );
}

/** The rules the last save would have written. */
function savedEntries() {
  const [variables] = mutate.mock.calls.at(-1) ?? [];
  return variables.request.setResourceAudienceForm;
}

describe("direct access rules", () => {
  it("reads as a sentence naming the level and the narrowing", () => {
    renderList([entry({ tools: ["search"] })]);

    expect(screen.getByText("Hana Sato")).toBeTruthy();
    expect(screen.getByText("connect")).toBeTruthy();
    expect(screen.getByText("search")).toBeTruthy();
  });

  it("says when nobody has been given direct access", () => {
    renderList([]);
    expect(screen.getByText(/Nobody has/)).toBeTruthy();
  });

  it("removes one rule by leaving it out of the replacement", () => {
    renderList([
      entry({}),
      entry({ principalUrn: "user:2", displayName: "Jonas" }),
    ]);

    fireEvent.click(screen.getByLabelText("Remove Hana Sato"));

    const form = savedEntries();
    expect(
      form.entries.map((e: { principalUrn: string }) => e.principalUrn),
    ).toEqual(["user:2"]);
    // The rules covering every server are not this surface's to write.
    expect(form.resourceId).toBe("server-1");
    expect(form.expectedVersion).toBe("v1");
  });

  it("keeps organization-level rules out of what it saves", () => {
    renderList([
      entry({}),
      entry({
        principalUrn: "role:global:1",
        kind: "role",
        displayName: "Admin",
        appliesTo: "all_resources",
        level: "manage",
      }),
    ]);

    fireEvent.click(screen.getByLabelText("Remove Hana Sato"));

    expect(savedEntries().entries).toEqual([]);
  });
});
