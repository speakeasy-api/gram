import { describe, expect, it } from "vitest";
import {
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
});
