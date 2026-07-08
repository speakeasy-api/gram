import type { AccessMember } from "@gram/client/models/components/accessmember.js";

/**
 * Resolve a chat's end user to an org member.
 *
 * Matches on the Gram user id (`AccessMember.id`) against the chat's `userId` —
 * this is what MCP/embed sessions authenticated as a known user carry, even
 * when their `externalUserId` holds an opaque provider id like a WorkOS
 * `user_…`. Dashboard chats stash the caller's email in `externalUserId`
 * instead, so we also fall back to an email match. Returns undefined for
 * external users who aren't org members.
 */
export function resolveChatOwner(
  members: AccessMember[] | undefined,
  chat: { userId?: string; externalUserId?: string },
): AccessMember | undefined {
  if (!members) return undefined;
  return members.find(
    (m) =>
      (!!chat.userId && m.id === chat.userId) ||
      (!!chat.externalUserId && m.email === chat.externalUserId),
  );
}
