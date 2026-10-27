import { expect, it } from "vitest";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import { assignmentExclusions } from "./assignmentExclusions";
const entries: ResourceAudienceEntry[] = [
  {
    principalUrn: "role:organization:engineer",
    kind: "role",
    displayName: "Engineer",
    level: "use",
    appliesTo: "all_resources",
  },
  {
    principalUrn: "user:someone-else",
    kind: "user",
    displayName: "Someone else",
    level: "use",
    appliesTo: "resource",
    tools: ["read_issue"],
  },
  {
    principalUrn: "role:organization:engineer",
    kind: "role",
    displayName: "Engineer",
    level: "blocked",
    appliesTo: "resource",
    dispositions: ["destructive"],
  },
];
it("adds only a connect exclusion without copying inherited grants or widening existing rules", () => {
  const written = assignmentExclusions(entries, ["role:organization:engineer"]);
  expect(written).toEqual(
    expect.arrayContaining([
      expect.objectContaining({
        principalUrn: "user:someone-else",
        level: "use",
        tools: ["read_issue"],
      }),
      ...["blocked"].map((level) => ({
        principalUrn: "role:organization:engineer",
        level,
        tools: [],
        dispositions: [],
      })),
    ]),
  );
  expect(written).toHaveLength(2);
  expect(
    written.some(
      (entry) =>
        entry.principalUrn === "role:organization:engineer" &&
        entry.level === "use",
    ),
  ).toBe(false);
  expect(entries[2]?.dispositions).toEqual(["destructive"]);
});
it("blocks only the selected user without altering role grants", () => {
  const written = assignmentExclusions(entries, ["user:selected-person"]);
  expect(
    written
      .filter((entry) => entry.principalUrn === "user:selected-person")
      .map((entry) => entry.level),
  ).toEqual(["blocked"]);
  expect(
    written.find(
      (entry) => entry.principalUrn === "role:organization:engineer",
    ),
  ).toMatchObject({ level: "blocked", dispositions: ["destructive"] });
});

it("rejects unknown annotations instead of broadening a preserved rule", () => {
  const unknown = {
    ...entries[1]!,
    dispositions: ["future_annotation"],
  } as unknown as ResourceAudienceEntry;
  expect(() =>
    assignmentExclusions([unknown], ["role:organization:engineer"]),
  ).toThrow();
});

it("rejects unknown access levels before preserving a rule", () => {
  const unknown = {
    ...entries[1]!,
    level: "future_level",
  } as unknown as ResourceAudienceEntry;
  expect(() =>
    assignmentExclusions([unknown], ["role:organization:engineer"]),
  ).toThrow();
});
