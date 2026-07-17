import type { AccessMember } from "@gram/client/models/components/accessmember.js";

export function resolveChatOwner(
  members: AccessMember[] | undefined,
  chat: { userId?: string; externalUserId?: string },
): AccessMember | undefined {
  if (!members) return undefined;

  return members.find(
    (member) =>
      (!!chat.userId && member.id === chat.userId) ||
      (!!chat.externalUserId && member.email === chat.externalUserId),
  );
}
