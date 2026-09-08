import { describe, expect, it } from "vitest";
import {
  mappingKindLabel,
  mappingLabel,
  unusedMappingTargets,
} from "./mappingHelpers";

describe("mappingHelpers", () => {
  it("labels group and attribute mappings", () => {
    expect(
      mappingLabel({
        kind: "group",
        groupName: "Engineering",
        groupId: "group-1",
        attributeKey: undefined,
        attributeValue: undefined,
        principalUrn: "directory_group:group-1",
      }),
    ).toBe("Engineering");
    expect(
      mappingLabel({
        kind: "attribute",
        groupName: undefined,
        groupId: undefined,
        attributeKey: "department",
        attributeValue: "Engineering",
        principalUrn: "directory_attribute:abc:def",
      }),
    ).toBe("department: Engineering");
    expect(mappingKindLabel("group")).toBe("Group");
    expect(mappingKindLabel("attribute")).toBe("Attribute");
  });

  it("omits already mapped targets and sorts groups first", () => {
    const options = unusedMappingTargets(
      [
        {
          id: "g2",
          name: "Sales",
          principalUrn: "directory_group:g2",
          memberCount: 4,
        },
        {
          id: "g1",
          name: "Engineering",
          principalUrn: "directory_group:g1",
          memberCount: 12,
        },
      ],
      [
        {
          key: "job_title",
          value: "I.T Admins",
          principalUrn: "directory_attribute:title",
          memberCount: 2,
        },
        {
          key: "department",
          value: "Engineering",
          principalUrn: "directory_attribute:dept",
          memberCount: 8,
        },
      ],
      ["directory_group:g2"],
    );

    expect(options.map((option) => option.principalUrn)).toEqual([
      "directory_group:g1",
      "directory_attribute:dept",
      "directory_attribute:title",
    ]);
  });
});
