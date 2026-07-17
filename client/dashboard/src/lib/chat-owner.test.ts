import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import { describe, expect, it } from "vitest";
import { resolveChatOwner } from "./chat-owner";

function member(overrides: Partial<AccessMember>): AccessMember {
  return {
    id: "gram-user-id",
    email: "member@example.com",
    name: "Example Member",
    joinedAt: new Date(0),
    principalUrn: "principal:user:gram-user-id",
    roleIds: [],
    ...overrides,
  };
}

const members = [
  member({ id: "gram-1", email: "ada@example.com", name: "Ada" }),
  member({ id: "gram-2", email: "grace@example.com", name: "Grace" }),
];

describe("resolveChatOwner", () => {
  it("matches a compliance chat using its resolved Gram user ID", () => {
    const owner = resolveChatOwner(members, {
      userId: "gram-2",
      externalUserId: "user_01HXXXXXXXXXXXXXXXXXXXXXXX",
    });

    expect(owner?.name).toBe("Grace");
  });

  it("matches dashboard chats using the external email", () => {
    const owner = resolveChatOwner(members, {
      externalUserId: "ada@example.com",
    });

    expect(owner?.name).toBe("Ada");
  });

  it("does not match an unresolved opaque external user ID", () => {
    expect(
      resolveChatOwner(members, {
        externalUserId: "user_01HXXXXXXXXXXXXXXXXXXXXXXX",
      }),
    ).toBeUndefined();
  });

  it("returns undefined before members load", () => {
    expect(resolveChatOwner(undefined, { userId: "gram-1" })).toBeUndefined();
  });
});
