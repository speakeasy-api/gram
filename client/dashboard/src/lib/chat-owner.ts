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

// Emails can differ only by casing between sources (WorkOS-synced members vs
// provider-reported identities), so all email comparisons are case-insensitive.
export function emailsMatch(
  a: string | undefined,
  b: string | undefined,
): boolean {
  return !!a && !!b && a.toLowerCase() === b.toLowerCase();
}

export function resolveChatOwner(
  members: AccessMember[] | undefined,
  chat: ChatIdentity,
): AccessMember | undefined {
  if (!members) return undefined;

  return members.find(
    (member) =>
      (!!chat.userId && member.id === chat.userId) ||
      emailsMatch(member.email, chat.externalUserId),
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
    emailsMatch(chat.externalUserId, currentUser.email);
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
  if (chat.userId) {
    return "This session's user couldn't be matched to a member of your organization. The user may have been removed from the organization or may not be provisioned in Gram.";
  }
  return "The AI provider didn't report a user identity for this session.";
}
