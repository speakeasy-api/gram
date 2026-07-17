import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import { describe, expect, it } from "vitest";
import { chatOwnerLabel, resolveChatOwner } from "./chat-owner";

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
const currentUser = { id: "gram-2", email: "grace@example.com" };

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

describe("chatOwnerLabel", () => {
  it("prefers a personal account email", () => {
    expect(
      chatOwnerLabel(
        members,
        { userId: "gram-2", externalUserId: "user_opaque" },
        currentUser,
        "personal@example.com",
      ),
    ).toBe("personal@example.com");
  });

  it("labels the current user as You before using their member name", () => {
    expect(
      chatOwnerLabel(
        members,
        { userId: "gram-2", externalUserId: "user_opaque" },
        currentUser,
      ),
    ).toBe("You");
  });

  it("uses another member's name", () => {
    expect(
      chatOwnerLabel(
        members,
        { userId: "gram-1", externalUserId: "user_opaque" },
        currentUser,
      ),
    ).toBe("Ada");
  });

  it("falls back to an unresolved external user ID", () => {
    expect(
      chatOwnerLabel(members, { externalUserId: "user_opaque" }, currentUser),
    ).toBe("user_opaque");
  });
});
