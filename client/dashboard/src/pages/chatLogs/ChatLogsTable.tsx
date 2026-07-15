import { AccountTypeIcon } from "@/components/account-type-icon";
import { personalAccountEmail } from "@/components/observe/account-display-utils";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Spinner } from "@/components/ui/spinner";
import { CopyButton } from "@/components/ui/copy-button";
import { cn } from "@/lib/utils";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import { useSession } from "@/contexts/Auth";
import type { ChatOverview } from "@gram/client/models/components/chatoverview.js";
import { Button } from "@/components/ui/button";
import { format } from "date-fns";
import { useState } from "react";
import {
  ChevronRight,
  DollarSign,
  Inbox,
  MessageSquare,
  Timer,
  Trash2,
  TriangleAlert,
} from "lucide-react";

// Label for a session's owner. Personal-account sessions show the account's own
// email (the session's user fields carry the attributed employee's WORK email,
// which hides which account was actually used — the adjacent AccountTypeIcon
// marks it personal). Otherwise the caller's own sessions show "You" (matched by
// internal user id, or by external user id when it equals their email —
// seeded/dashboard chats carry the email there). Everyone else falls back to the
// external user id, or "anonymous" when there is none.
function ownerLabel(
  chat: ChatOverview,
  user: { id: string; email: string },
): string {
  const accountEmail = personalAccountEmail(chat);
  if (accountEmail) return accountEmail;
  const isMe =
    (!!chat.userId && chat.userId === user.id) ||
    (!!chat.externalUserId && chat.externalUserId === user.email);
  if (isMe) return "You";
  return chat.externalUserId || "anonymous";
}

interface ChatLogsTableProps {
  chats: ChatOverview[];
  selectedChatId?: string;
  onSelectChat: (chat: ChatOverview) => void;
  onDeleteChat: (chatId: string) => void;
  isLoading: boolean;
  error: Error | null;
}

function getTraceId(chatId: string): string {
  return chatId.slice(0, 8);
}

function RiskIndicator({ count, size = 44 }: { count: number; size?: number }) {
  const hasRisk = count > 0;
  return (
    <SimpleTooltip
      tooltip={
        hasRisk
          ? `${count} risk finding${count === 1 ? "" : "s"} on this session`
          : "No risk findings on this session"
      }
    >
      <div className="flex flex-col items-center gap-1">
        <div
          className={cn(
            "flex items-center justify-center rounded-full border-[3px]",
            hasRisk
              ? "border-destructive/40 text-destructive bg-destructive/5"
              : "border-muted-foreground/30 text-muted-foreground/70",
          )}
          style={{ width: size, height: size }}
        >
          <span className="text-sm font-semibold tabular-nums">{count}</span>
        </div>
        <span className="text-muted-foreground font-mono text-[9px] tracking-[0.08em] uppercase">
          Risk
        </span>
      </div>
    </SimpleTooltip>
  );
}

function formatDuration(chat: ChatOverview): string {
  // Use lastMessageTimestamp if available, otherwise fall back to updatedAt
  const endTime = chat.lastMessageTimestamp ?? chat.updatedAt;
  const seconds = Math.round(
    (endTime.getTime() - chat.createdAt.getTime()) / 1000,
  );
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  return remainingSeconds > 0
    ? `${minutes}m ${remainingSeconds}s`
    : `${minutes}m`;
}

export function ChatLogsTable({
  chats,
  selectedChatId,
  onSelectChat,
  onDeleteChat,
  isLoading,
  error,
}: ChatLogsTableProps): JSX.Element {
  const { user } = useSession();
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null);
  if (isLoading && chats.length === 0) {
    return (
      <div className="flex h-64 items-center justify-center">
        <div className="text-muted-foreground flex items-center gap-2 text-sm">
          <Spinner className="mr-0 size-4" />
          Loading traces...
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex h-64 items-center justify-center px-4">
        <InlineEmptyState
          icon={<TriangleAlert />}
          title="Failed to load traces"
          description={error.message}
        />
      </div>
    );
  }

  if (chats.length === 0) {
    return (
      <div className="flex h-64 items-center justify-center px-4">
        <InlineEmptyState
          icon={<Inbox />}
          title="No traces found"
          description="Try adjusting your filters or time range"
        />
      </div>
    );
  }

  return (
    <>
      <div className="divide-border/50 divide-y">
        {chats.map((chat) => {
          const isSelected = selectedChatId === chat.id;
          const source = chat.source;
          const riskCount = chat.riskFindingsCount ?? 0;
          const lastActivityTimestamp =
            chat.lastMessageTimestamp ?? chat.createdAt;

          return (
            <button
              key={chat.id}
              onClick={() => onSelectChat(chat)}
              className={cn(
                "group w-full px-5 py-4 text-left transition-all duration-150",
                "hover:bg-muted/50",
                "focus-visible:bg-muted/50 focus:outline-none",
                isSelected && "bg-primary/3 hover:bg-primary/5",
              )}
            >
              <div className="flex items-center gap-5">
                {/* Left: Risk findings indicator */}
                <div className="shrink-0">
                  <RiskIndicator count={riskCount} size={44} />
                </div>

                {/* Center: Main content */}
                <div className="min-w-0 flex-1">
                  {/* Header row */}
                  <div className="mb-1.5 flex items-center gap-2">
                    <span className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
                      {getTraceId(chat.id)}
                    </span>
                    <CopyButton
                      text={chat.id}
                      tooltip="Copy Chat ID"
                      size="inline"
                      className="opacity-50 hover:opacity-100"
                    />
                    <span className="text-muted-foreground/40">·</span>
                    <span className="text-muted-foreground text-sm">
                      Created {format(chat.createdAt, "MMM d, HH:mm")}
                    </span>
                    <span className="text-muted-foreground/40">·</span>
                    <span className="text-muted-foreground text-sm">
                      Last activity{" "}
                      {format(lastActivityTimestamp, "MMM d, HH:mm")}
                    </span>
                  </div>

                  {/* Title */}
                  <Heading
                    variant="h6"
                    className="text-foreground mb-2 line-clamp-2 leading-snug font-medium"
                  >
                    {chat.title}
                  </Heading>

                  {/* Metadata row */}
                  <div className="text-muted-foreground flex items-center gap-4 text-sm">
                    <span className="flex items-center gap-1.5">
                      <AccountTypeIcon accountType={chat.accountType} />
                      <span className="max-w-[120px] truncate">
                        {ownerLabel(chat, user)}
                      </span>
                    </span>
                    {source && (
                      <span className="flex items-center gap-1.5">
                        <HookSourceIcon source={source} className="size-4" />
                        {source}
                      </span>
                    )}
                    <span className="flex items-center gap-1.5">
                      <Timer className="size-4 opacity-60" />
                      <span className="tabular-nums">
                        {formatDuration(chat)}
                      </span>
                    </span>
                    <span className="flex items-center gap-1.5">
                      <MessageSquare className="size-4 opacity-60" />
                      <span className="tabular-nums">
                        {chat.numMessages}
                      </span>{" "}
                      messages
                    </span>
                    {chat.totalCost !== undefined && chat.totalCost > 0 && (
                      <span className="flex items-center gap-0">
                        <DollarSign className="size-4 opacity-60" />
                        <span className="tabular-nums">
                          {chat.totalCost.toFixed(4)}
                        </span>
                      </span>
                    )}
                  </div>
                </div>

                {/* Right: Delete + Chevron */}
                <div className="flex shrink-0 items-center gap-1 pt-2">
                  <span
                    role="button"
                    tabIndex={0}
                    onClick={(e) => {
                      e.stopPropagation();
                      setDeleteConfirmId(chat.id);
                    }}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.stopPropagation();
                        setDeleteConfirmId(chat.id);
                      }
                    }}
                    className="hover:bg-destructive/10 text-muted-foreground hover:text-destructive p-1 opacity-0 transition-all group-hover:opacity-100"
                    aria-label="Delete chat"
                  >
                    <Trash2 className="size-4" />
                  </span>
                  <ChevronRight
                    className={cn(
                      "size-4 transition-colors",
                      isSelected
                        ? "text-foreground/60"
                        : "text-muted-foreground/40",
                    )}
                  />
                </div>
              </div>
            </button>
          );
        })}
      </div>

      <Dialog
        open={deleteConfirmId !== null}
        onOpenChange={(open) => {
          void (!open && setDeleteConfirmId(null));
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Delete chat session</Dialog.Title>
            <Dialog.Description>
              Are you sure you want to delete this chat session? This action
              cannot be undone.
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Dialog.Close asChild>
              <Button variant="secondary">Cancel</Button>
            </Dialog.Close>
            <Button
              variant="destructive-primary"
              onClick={() => {
                if (deleteConfirmId) {
                  onDeleteChat(deleteConfirmId);
                }
                setDeleteConfirmId(null);
              }}
            >
              Delete
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
