import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { PluginAudience } from "@gram/client/models/components/pluginaudience.js";
import type { Role } from "@gram/client/models/components/role.js";
import { describe, expect, it } from "vitest";
import {
  audienceKindForPrincipal,
  audienceMapByUrn,
  describePrincipal,
  isIndividualUserAssignmentPrincipal,
  isEveryoneAssignmentPrincipal,
  memberMapByUrn,
  normalizeToPrincipalUrn,
  roleMapByUrn,
  selectMutuallyExclusivePluginAudiences,
} from "./principals";

const role = {
  principalUrn: "role:organization:abc",
  name: "Engineering",
} as Role;

const member = {
  principalUrn: "user:u-123",
  name: "Jane Doe",
  email: "jane@corp.com",
} as AccessMember;

const roleByUrn = roleMapByUrn([role]);
const memberByUrn = memberMapByUrn([member]);
const directoryAudience = {
  principalUrn: "directory_group:00000000-0000-0000-0000-000000000000",
  displayName: "Engineering",
  kind: "directory_group",
  memberCount: 2,
} as PluginAudience;

describe("normalizeToPrincipalUrn", () => {
  it("passes through the wildcard", () => {
    expect(normalizeToPrincipalUrn("*")).toBe("*");
  });

  it("passes through known URN prefixes unchanged", () => {
    expect(normalizeToPrincipalUrn("role:organization:abc")).toBe(
      "role:organization:abc",
    );
    expect(normalizeToPrincipalUrn("user:u-123")).toBe("user:u-123");
    expect(normalizeToPrincipalUrn("email:jane@corp.com")).toBe(
      "email:jane@corp.com",
    );
  });

  it("treats a bare email as an email principal, lowercased and trimmed", () => {
    expect(normalizeToPrincipalUrn("  Jane@Corp.com ")).toBe(
      "email:jane@corp.com",
    );
  });

  it("rejects a non-email bare value", () => {
    expect(normalizeToPrincipalUrn("not-an-email")).toBeNull();
    expect(normalizeToPrincipalUrn("")).toBeNull();
  });

  it("validates the address even when the email: prefix is already present", () => {
    expect(normalizeToPrincipalUrn("email:not-an-address")).toBeNull();
    expect(normalizeToPrincipalUrn("email:")).toBeNull();
    expect(normalizeToPrincipalUrn("email:Jane@Corp.com")).toBe(
      "email:jane@corp.com",
    );
  });

  it("rejects role:/user: prefixes with an empty id", () => {
    expect(normalizeToPrincipalUrn("role:")).toBeNull();
    expect(normalizeToPrincipalUrn("user:")).toBeNull();
    expect(normalizeToPrincipalUrn("  role: ")).toBeNull();
  });
});

describe("describePrincipal", () => {
  it("labels the wildcard and user:all as everyone", () => {
    expect(describePrincipal("*", roleByUrn, memberByUrn)).toEqual({
      kind: "everyone",
      label: "Everyone",
    });
    expect(describePrincipal("user:all", roleByUrn, memberByUrn)).toEqual({
      kind: "everyone",
      label: "All users",
    });
  });

  it("resolves a role URN to its name", () => {
    expect(
      describePrincipal("role:organization:abc", roleByUrn, memberByUrn),
    ).toEqual({ kind: "role", label: "Engineering" });
  });

  it("resolves a user URN to the member name", () => {
    expect(describePrincipal("user:u-123", roleByUrn, memberByUrn)).toEqual({
      kind: "user",
      label: "Jane Doe",
    });
  });

  it("shows the address for an email principal", () => {
    expect(
      describePrincipal("email:x@corp.com", roleByUrn, memberByUrn),
    ).toEqual({ kind: "email", label: "x@corp.com" });
  });

  it("falls back to the raw URN when unresolvable", () => {
    expect(
      describePrincipal("role:organization:missing", roleByUrn, memberByUrn),
    ).toEqual({ kind: "role", label: "role:organization:missing" });
  });

  it("resolves a directory audience to its display name", () => {
    expect(
      describePrincipal(
        directoryAudience.principalUrn,
        roleByUrn,
        memberByUrn,
        audienceMapByUrn([directoryAudience]),
      ),
    ).toEqual({ kind: "directory_group", label: "Engineering" });
  });

  it("labels an unavailable directory audience and retains its kind", () => {
    const unavailableUrn = "directory_group:deleted-group";
    expect(describePrincipal(unavailableUrn, roleByUrn, memberByUrn)).toEqual({
      kind: "directory_group",
      label: `Unavailable directory group (${unavailableUrn})`,
    });
    expect(audienceKindForPrincipal(unavailableUrn, new Map())).toBe(
      "directory_group",
    );
  });
});

describe("isIndividualUserAssignmentPrincipal", () => {
  it("includes individual user and email assignments but not user:all", () => {
    expect(isIndividualUserAssignmentPrincipal("user:u-123")).toBe(true);
    expect(isIndividualUserAssignmentPrincipal("email:jane@corp.com")).toBe(
      true,
    );
    expect(isIndividualUserAssignmentPrincipal("user:all")).toBe(false);
  });
});

describe("plugin audience selection", () => {
  it("clears targeted audiences when everyone is added", () => {
    expect(
      selectMutuallyExclusivePluginAudiences(
        ["role:organization:abc", "user:u-123", "legacy:target"],
        ["role:organization:abc", "user:u-123", "legacy:target", "*"],
      ),
    ).toEqual(["*"]);
  });

  it("preserves a preexisting mixed selection until an audience is added", () => {
    const existing = ["*", "role:organization:abc", "user:u-123"];

    expect(selectMutuallyExclusivePluginAudiences(existing, existing)).toEqual(
      existing,
    );
  });

  it("clears everyone when a targeted audience is added", () => {
    expect(
      selectMutuallyExclusivePluginAudiences(
        ["*"],
        ["*", "directory_group:example"],
      ),
    ).toEqual(["directory_group:example"]);
  });

  it("retains every targeted audience when another is added", () => {
    expect(
      selectMutuallyExclusivePluginAudiences(
        ["role:organization:abc"],
        ["role:organization:abc", "user:u-123"],
      ),
    ).toEqual(["role:organization:abc", "user:u-123"]);
  });

  it("keeps member-wide and non-member email assignments together", () => {
    expect(
      selectMutuallyExclusivePluginAudiences(
        ["user:all"],
        ["user:all", "email:external@example.test"],
      ),
    ).toEqual(["user:all", "email:external@example.test"]);
  });

  it("clears member-scoped assignments when all members is added", () => {
    expect(
      selectMutuallyExclusivePluginAudiences(
        [
          "role:organization:abc",
          "user:u-123",
          "email:external@example.test",
          "directory_group:example",
        ],
        [
          "role:organization:abc",
          "user:u-123",
          "email:external@example.test",
          "directory_group:example",
          "user:all",
        ],
      ),
    ).toEqual([
      "email:external@example.test",
      "directory_group:example",
      "user:all",
    ]);
  });

  it("clears all members when a member-scoped assignment is added", () => {
    expect(
      selectMutuallyExclusivePluginAudiences(
        ["user:all", "email:external@example.test"],
        ["user:all", "email:external@example.test", "user:u-123"],
      ),
    ).toEqual(["email:external@example.test", "user:u-123"]);
  });

  it("keeps all members when a directory audience is added", () => {
    expect(
      selectMutuallyExclusivePluginAudiences(
        ["user:all"],
        ["user:all", "directory_group:example"],
      ),
    ).toEqual(["user:all", "directory_group:example"]);
  });

  it("keeps an existing mixed assignment removable without rewriting it", () => {
    expect(
      selectMutuallyExclusivePluginAudiences(["*", "user:u-123"], ["*"]),
    ).toEqual(["*"]);
  });

  it("recognizes both principals that target everyone", () => {
    expect(isEveryoneAssignmentPrincipal("*")).toBe(true);
    expect(isEveryoneAssignmentPrincipal("user:all")).toBe(true);
  });
});
