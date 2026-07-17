import type { AccessMember } from "@gram/client/models/components/accessmember.js";

type ChatIdentity = {
  userId?: string;
  externalUserId?: string;
};

export function resolveChatOwner(
  members: AccessMember[] | undefined,
  chat: ChatIdentity,
): AccessMember | undefined {
  if (!members) return undefined;

  return members.find(
    (member) =>
      (!!chat.userId && member.id === chat.userId) ||
      (!!chat.externalUserId && member.email === chat.externalUserId),
  );
}

export function chatOwnerLabel(
  members: AccessMember[] | undefined,
  chat: ChatIdentity,
  currentUser: { id: string; email: string },
  accountEmail?: string,
): string {
  if (accountEmail) return accountEmail;

  const isCurrentUser =
    (!!chat.userId && chat.userId === currentUser.id) ||
    (!!chat.externalUserId && chat.externalUserId === currentUser.email);
  if (isCurrentUser) return "You";

  const member = resolveChatOwner(members, chat);
  if (member) return member.name || member.email;

  return chat.externalUserId || "anonymous";
}
