import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import { describe, expect, it } from "vitest";
import {
  describePrincipal,
  memberMapByUrn,
  normalizeToPrincipalUrn,
  roleMapByUrn,
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
});
