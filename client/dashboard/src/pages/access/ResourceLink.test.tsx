import { cleanup, render, screen } from "@testing-library/react";
import type { ChallengeBucket } from "@gram/client/models/components/challengebucket.js";
import { afterEach, describe, expect, it } from "vitest";
import { MemoryRouter } from "react-router";
import { ResourceLink } from "./ResourceLink";

const projectMap = new Map<string, { slug: string; name: string }>([
  ["proj-1", { slug: "default", name: "Default" }],
]);
const emptyToolsetMap = new Map<
  string,
  { slug: string; name: string; projectId: string }
>();
const emptyMcpServerMap = new Map<
  string,
  { slug?: string; name?: string; projectId: string }
>();

function makeBucket(overrides: Partial<ChallengeBucket> = {}): ChallengeBucket {
  return {
    id: "bucket-1",
    challengeCount: 1,
    challengeIds: ["ch-1"],
    evaluatedGrantCount: 1,
    firstSeen: new Date("2026-05-01T00:00:00Z"),
    lastSeen: new Date("2026-05-01T00:00:00Z"),
    matchedGrantCount: 0,
    operation: "require",
    organizationId: "org-1",
    outcome: "deny",
    principalType: "user",
    principalUrn: "user:u1",
    reason: "scope_unsatisfied",
    roleSlugs: [],
    scope: "skill:read",
    ...overrides,
  };
}

function renderLink(bucket: ChallengeBucket) {
  return render(
    <MemoryRouter>
      <ResourceLink
        challenge={bucket}
        orgSlug="acme"
        projectMap={projectMap}
        toolsetMap={emptyToolsetMap}
        mcpServerMap={emptyMcpServerMap}
      />
    </MemoryRouter>,
  );
}

afterEach(cleanup);

describe("ResourceLink", () => {
  it("links skill resources to the project's skills page", () => {
    renderLink(
      makeBucket({
        scope: "skill:read",
        resourceKind: "skill",
        resourceId: "proj-1",
        projectId: "proj-1",
      }),
    );
    const link = screen.getByRole("link", { name: /Default/ });
    expect(link.getAttribute("href")).toBe("/acme/projects/default/skills");
  });

  it("links environment resources to the project's environments page", () => {
    renderLink(
      makeBucket({
        scope: "environment:write",
        resourceKind: "environment",
        // A specific environment id, not a project id — resolved via the
        // bucket's projectId.
        resourceId: "env-abc",
        projectId: "proj-1",
      }),
    );
    const link = screen.getByRole("link", { name: /Default/ });
    expect(link.getAttribute("href")).toBe(
      "/acme/projects/default/environments",
    );
  });

  it("falls back to an id chip when the project cannot be resolved", () => {
    renderLink(
      makeBucket({
        scope: "environment:write",
        resourceKind: "environment",
        resourceId: "env-orphan",
        projectId: "unknown-project",
      }),
    );
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.getByTitle("env-orphan").textContent).toContain("env-orph");
  });
});
