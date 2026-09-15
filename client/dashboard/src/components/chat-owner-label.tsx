import { IdentityLink } from "@/components/identity-link";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import {
  chatOwnerDisplay,
  resolveChatOwner,
  unresolvedChatOwnerTooltip,
} from "@/lib/chat-owner";
import type { IdentityRef } from "@/lib/identity-urn";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { JSX } from "react";

/**
 * Renders a chat session's owner. When the owner can't be resolved to a known
 * identity (the raw provider user ID or "anonymous" is shown), the label gets
 * a tooltip explaining why.
 */
export function ChatOwnerLabel({
  members,
  chat,
  currentUser,
  accountEmail,
}: {
  members: AccessMember[] | undefined;
  chat: { userId?: string; externalUserId?: string };
  currentUser: { id: string; email: string };
  accountEmail?: string;
}): JSX.Element {
  const owner = chatOwnerDisplay(members, chat, currentUser, accountEmail);
  // The label names whichever member the chat matched, and the match can come
  // through the reported agent id rather than the chat's own user id — a stale
  // or non-member user id would otherwise put someone else's page behind that
  // name. With no match the label is the reported id, and so is the link.
  const member = resolveChatOwner(members, chat);
  const identifier: IdentityRef | null = member
    ? { userId: member.id }
    : chat.externalUserId
      ? { externalUserId: chat.externalUserId }
      : chat.userId
        ? { userId: chat.userId }
        : null;

  // While the members query is still loading, matching hasn't been attempted
  // yet — render plainly instead of claiming the user couldn't be matched.
  if (owner.resolved || !members) {
    return <IdentityLink identifier={identifier}>{owner.label}</IdentityLink>;
  }

  return (
    <SimpleTooltip tooltip={unresolvedChatOwnerTooltip(chat)}>
      <span
        tabIndex={0}
        className="cursor-help underline decoration-dotted underline-offset-2"
      >
        <IdentityLink identifier={identifier}>{owner.label}</IdentityLink>
      </span>
    </SimpleTooltip>
  );
}
