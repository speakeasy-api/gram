import { IdentityLink } from "@/components/identity-link";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { chatOwnerDisplay, unresolvedChatOwnerTooltip } from "@/lib/chat-owner";
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
  // A chat carries the Gram user when the owner is a member and the reported
  // agent id otherwise; either reaches the same identity page.
  const identifier = chat.userId
    ? { userId: chat.userId }
    : chat.externalUserId
      ? { externalUserId: chat.externalUserId }
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
