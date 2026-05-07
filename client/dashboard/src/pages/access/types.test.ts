import { describe, expect, it } from "vitest";
import { toRoleSlug } from "./types";

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

describe("system role slug resolution", () => {
  // Mirrors the GrantDrawer logic: system roles use toLowerCase(),
  // custom roles use toRoleSlug().
  function resolveSlug(name: string, isSystem: boolean): string {
    return isSystem ? name.toLowerCase() : toRoleSlug(name);
  }

  it("system Admin → admin (no org- prefix)", () => {
    expect(resolveSlug("Admin", true)).toBe("admin");
  });

  it("system Member → member (no org- prefix)", () => {
    expect(resolveSlug("Member", true)).toBe("member");
  });

  it("custom Editor → org-editor", () => {
    expect(resolveSlug("Editor", false)).toBe("org-editor");
  });

  it("custom role with spaces → org-prefixed slug", () => {
    expect(resolveSlug("API Developer", false)).toBe("org-api-developer");
  });
});
