import type { Skill } from "@gram/client/models/components/skill.js";
import { describe, expect, it } from "vitest";
import { filterSkills, skillCountLabel } from "./skills-list-helpers";

function skill(overrides: Partial<Skill>): Skill {
  return {
    id: "skill_a",
    projectId: "project_a",
    name: "release-notes",
    displayName: "Release Notes",
    summary: "Draft customer release notes",
    sourceKind: "manual",
    classification: "custom",
    latestVersionId: "version_a",
    versionCount: 1,
    createdAt: new Date("2026-07-16T00:00:00Z"),
    updatedAt: new Date("2026-07-16T00:00:00Z"),
    ...overrides,
  };
}

describe("SkillsList filtering", () => {
  const skills = [
    skill({ id: "a" }),
    skill({
      id: "b",
      name: "incident-response",
      displayName: "Incident Response",
      summary: "Handle incidents",
      sourceKind: "imported",
      classification: "verified",
    }),
  ];

  it("searches display name, normalized name, and summary", () => {
    expect(
      filterSkills(skills, "release notes", [], []).map((item) => item.id),
    ).toEqual(["a"]);
    expect(
      filterSkills(skills, "incident-response", [], []).map((item) => item.id),
    ).toEqual(["b"]);
    expect(
      filterSkills(skills, "customer", [], []).map((item) => item.id),
    ).toEqual(["a"]);
  });

  it("combines source and classification filters", () => {
    expect(
      filterSkills(skills, "", ["imported"], ["verified"]).map(
        (item) => item.id,
      ),
    ).toEqual(["b"]);
    expect(filterSkills(skills, "", ["manual"], ["verified"])).toEqual([]);
  });

  it("never labels loaded pages as the project-wide total", () => {
    expect(
      skillCountLabel({
        active: false,
        hasNextPage: true,
        incomplete: false,
        loadedCount: 200,
        resultCount: 200,
      }),
    ).toBe("200 loaded");
    expect(
      skillCountLabel({
        active: true,
        hasNextPage: true,
        incomplete: false,
        loadedCount: 200,
        resultCount: 3,
      }),
    ).toBe("Searching 200 loaded");
    expect(
      skillCountLabel({
        active: true,
        hasNextPage: false,
        incomplete: false,
        loadedCount: 240,
        resultCount: 3,
      }),
    ).toBe("3 skills");
    expect(
      skillCountLabel({
        active: true,
        hasNextPage: true,
        incomplete: true,
        loadedCount: 200,
        resultCount: 0,
      }),
    ).toBe("0 matching loaded");
  });
});
