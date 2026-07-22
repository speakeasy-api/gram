import type { AccessMember } from "@gram/client/models/components/accessmember.js";

type ChatIdentity = {
  userId?: string;
  externalUserId?: string;
};

export type ChatOwnerDisplay = {
  label: string;
  /**
   * True when the label is a known identity (personal-account email, the
   * viewer, or an org member). False when we fell back to the raw external
   * user ID or "anonymous".
   */
  resolved: boolean;
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

export function chatOwnerDisplay(
  members: AccessMember[] | undefined,
  chat: ChatIdentity,
  currentUser: { id: string; email: string },
  accountEmail?: string,
): ChatOwnerDisplay {
  if (accountEmail) return { label: accountEmail, resolved: true };

  const isCurrentUser =
    (!!chat.userId && chat.userId === currentUser.id) ||
    (!!chat.externalUserId && chat.externalUserId === currentUser.email);
  if (isCurrentUser) return { label: "You", resolved: true };

  const member = resolveChatOwner(members, chat);
  if (member) return { label: member.name || member.email, resolved: true };

  return { label: chat.externalUserId || "anonymous", resolved: false };
}

export function chatOwnerLabel(
  members: AccessMember[] | undefined,
  chat: ChatIdentity,
  currentUser: { id: string; email: string },
  accountEmail?: string,
): string {
  return chatOwnerDisplay(members, chat, currentUser, accountEmail).label;
}

export function unresolvedChatOwnerTooltip(chat: ChatIdentity): string {
  if (chat.externalUserId) {
    return "This session's user couldn't be matched to a member of your organization, so the user ID reported by the AI provider is shown instead. The user may not be provisioned in Gram or may have been removed from the organization.";
  }
  return "The AI provider didn't report a user identity for this session.";
}
