import { describe, expect, it } from "vitest";
import {
  actorScopeSummaries,
  isUnrestrictedResourceType,
  toRoleSlug,
  unrestrictedResourceLabel,
} from "./types";

describe("toRoleSlug", () => {
  it("adds org- prefix to plain name", () => {
    expect(toRoleSlug("Editor")).toBe("org-editor");
  });

  it("does not double-prefix org- names", () => {
    expect(toRoleSlug("org-editor")).toBe("org-editor");
  });

  it("replaces spaces with hyphens", () => {
    expect(toRoleSlug("Project Manager")).toBe("org-project-manager");
  });

  it("replaces underscores with hyphens", () => {
    expect(toRoleSlug("team_lead")).toBe("org-team-lead");
  });

  it("strips special characters", () => {
    expect(toRoleSlug("QA & Testing!")).toBe("org-qa-testing");
  });

  it("collapses consecutive hyphens/spaces", () => {
    expect(toRoleSlug("super   admin")).toBe("org-super-admin");
  });

  it("trims leading/trailing hyphens", () => {
    expect(toRoleSlug("-reviewer-")).toBe("org-reviewer");
  });

  it("handles single word", () => {
    expect(toRoleSlug("viewer")).toBe("org-viewer");
  });
});

describe("unrestricted resource types", () => {
  it("treats agents as unrestricted", () => {
    expect(isUnrestrictedResourceType("agent")).toBe(true);
    expect(unrestrictedResourceLabel("agent")).toBe("All agents");
  });

  it("offers logs access as a whole-organization grant", () => {
    expect(isUnrestrictedResourceType("logs")).toBe(true);
    expect(unrestrictedResourceLabel("logs")).toBe("All activity");
  });
});

describe("actorScopeSummaries", () => {
  it("is empty for an unrestricted grant", () => {
    expect(actorScopeSummaries(null)).toEqual([]);
    expect(
      actorScopeSummaries([{ resourceKind: "logs", resourceId: "*" }]),
    ).toEqual([]);
  });

  it("describes one narrowing per selector", () => {
    expect(
      actorScopeSummaries([
        {
          resourceKind: "logs",
          resourceId: "*",
          actorDepartment: "Engineering",
        },
        { resourceKind: "logs", resourceId: "*", actorGroup: "platform-leads" },
      ]),
    ).toEqual(["department Engineering", "group platform-leads"]);
  });

  it("joins the dimensions of one selector, which must all hold", () => {
    expect(
      actorScopeSummaries([
        {
          resourceKind: "logs",
          resourceId: "*",
          actorDepartment: "Engineering",
          actorRole: "member",
        },
      ]),
    ).toEqual(["department Engineering and role member"]);
  });
});
