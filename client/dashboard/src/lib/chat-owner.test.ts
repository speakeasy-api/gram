import { describe, expect, it } from "vitest";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import { resolveChatOwner } from "./chat-owner";

function member(overrides: Partial<AccessMember>): AccessMember {
  return {
    id: "user-gram-id",
    email: "member@example.com",
    name: "Ada Member",
    joinedAt: new Date(0),
    principalUrn: "principal:user:user-gram-id",
    roleIds: [],
    ...overrides,
  };
}

describe("resolveChatOwner", () => {
  const members = [
    member({ id: "gram-1", email: "ada@example.com", name: "Ada" }),
    member({ id: "gram-2", email: "grace@example.com", name: "Grace" }),
  ];

  it("matches on Gram user id (e.g. a session with an opaque external id)", () => {
    const owner = resolveChatOwner(members, {
      userId: "gram-2",
      externalUserId: "user_01HXXXXXXXXXXXXXXXXXXXXXXX",
    });
    expect(owner?.name).toBe("Grace");
  });

  it("falls back to an email match (dashboard chats stash email here)", () => {
    const owner = resolveChatOwner(members, {
      externalUserId: "ada@example.com",
    });
    expect(owner?.name).toBe("Ada");
  });

  it("returns undefined for an external user who is not an org member", () => {
    expect(
      resolveChatOwner(members, {
        externalUserId: "user_01HXXXXXXXXXXXXXXXXXXXXXXX",
      }),
    ).toBeUndefined();
  });

  it("does not match an empty userId against a member with no Gram id", () => {
    const withEmptyId = [member({ id: "", email: "pending@example.com" })];
    expect(resolveChatOwner(withEmptyId, { userId: "" })).toBeUndefined();
  });

  it("returns undefined when members have not loaded", () => {
    expect(resolveChatOwner(undefined, { userId: "gram-1" })).toBeUndefined();
  });
});
