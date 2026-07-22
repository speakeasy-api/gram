import { SimpleTooltip } from "@/components/ui/tooltip";
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

  if (owner.resolved) return <>{owner.label}</>;

  return (
    <SimpleTooltip tooltip={unresolvedChatOwnerTooltip(chat)}>
      <span className="cursor-help underline decoration-dotted underline-offset-2">
        {owner.label}
      </span>
    </SimpleTooltip>
  );
}
