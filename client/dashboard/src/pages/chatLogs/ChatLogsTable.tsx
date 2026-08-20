import { AccountTypeIcon } from "@/components/account-type-icon";
import { ChatOwnerLabel } from "@/components/chat-owner-label";
import { personalAccountEmail } from "@/components/observe/account-display-utils";
import { TableRowContextMenu } from "@/components/table-row-context-menu";
import { Dialog } from "@/components/ui/Dialog";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { formatChatSource } from "@/lib/formatPlatform";
import { cn } from "@/lib/utils";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import { WorkUnitsRowMetrics } from "@/pages/chatLogs/WorkUnitsMetrics";
import { useSession } from "@/contexts/Auth";
import type { ChatOverview } from "@gram/client/models/components/chatoverview.js";
import { useChatSetPinnedMutation } from "@gram/client/react-query/chatSetPinned.js";
import { invalidateAllListChats } from "@gram/client/react-query/listChats.js";
import { useListChatSessionLinks } from "@gram/client/react-query/listChatSessionLinks.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { useQueryClient } from "@tanstack/react-query";
import { format } from "date-fns";
import { ArrowUpRight, CircleDashed, GitBranch, Pin } from "lucide-react";
import { useCallback, useMemo, useState, type MouseEvent } from "react";
import { toast } from "sonner";
import { formatPlatform } from "@/lib/formatPlatform";
import { summarizeLineage, type LineageSummary } from "./sessionLinks";

interface ChatLogsTableProps {
  chats: ChatOverview[];
  selectedChatId?: string;
  onSelectChat: (chat: ChatOverview) => void;
  onDeleteChat: (chatId: string) => void;
  isLoading: boolean;
  error: Error | null;
  emptyState?: {
    title: string;
    description: string;
  };
}

function getTraceId(chatId: string): string {
  return chatId.slice(0, 8);
}

function RiskIndicator({ count }: { count: number }) {
  const hasRisk = count > 0;
  return (
    <SimpleTooltip
      tooltip={
        hasRisk
          ? `${count} risk finding${count === 1 ? "" : "s"} on this session`
          : "No risk findings on this session"
      }
    >
      <div className="flex w-11 flex-col items-center gap-0.5">
        <span className="text-eyebrow">Risk</span>
        <span
          className={cn(
            "font-display text-2xl leading-none font-thin tabular-nums",
            hasRisk ? "text-destructive" : "text-muted-foreground/70",
          )}
        >
          {count}
        </span>
      </div>
    </SimpleTooltip>
  );
}

// listHarnesses renders a harness list for a lineage tooltip ("Claude Code",
// "Claude Code and Cursor").
function listHarnesses(harnesses: string[]): string {
  const labels = [...new Set(harnesses.map(formatPlatform))];
  if (labels.length <= 1) return labels[0] ?? "";
  return `${labels.slice(0, -1).join(", ")} and ${labels[labels.length - 1]}`;
}

// SessionLineageIcons is the at-a-glance lineage cluster on a session row:
// one icon per state — continued elsewhere (navigable continuation exists),
// derived from an earlier session, and moved with no captured continuation.
// The cluster sits above the row's stretched select button (tooltips need
// pointer events), so clicking it forwards to the same row selection; the
// stretched button remains the tab stop, hence tabIndex={-1}.
function SessionLineageIcons({
  summary,
  onActivate,
}: {
  summary?: LineageSummary;
  onActivate: () => void;
}) {
  if (!summary) return null;
  return (
    <button
      type="button"
      tabIndex={-1}
      onClick={onActivate}
      aria-label="Open session details"
      className="text-muted-foreground pointer-events-auto mt-0.5 inline-flex shrink-0 cursor-pointer items-center gap-1"
    >
      {summary.continuedIn.length > 0 && (
        <SimpleTooltip
          tooltip={`Continued in ${listHarnesses(summary.continuedIn)}`}
        >
          <ArrowUpRight
            className="size-3.5"
            aria-label="Continued in another harness"
          />
        </SimpleTooltip>
      )}
      {summary.derived && (
        <SimpleTooltip tooltip="Derived from an earlier session">
          <GitBranch
            className="size-3.5"
            aria-label="Derived from an earlier session"
          />
        </SimpleTooltip>
      )}
      {summary.danglingIn.length > 0 && (
        <SimpleTooltip
          tooltip={`Moved to ${listHarnesses(summary.danglingIn)} — continuation not yet captured`}
        >
          <CircleDashed
            className="size-3.5"
            aria-label="Moved, continuation not yet captured"
          />
        </SimpleTooltip>
      )}
    </button>
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

function SessionPinButton({
  chatId,
  pinned,
}: {
  chatId: string;
  pinned: boolean;
}) {
  const queryClient = useQueryClient();
  const setPinned = useChatSetPinnedMutation();

  const toggle = (e: MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setPinned.mutate(
      {
        request: { setPinnedRequestBody: { id: chatId, pinned: !pinned } },
      },
      {
        onSettled: () => void invalidateAllListChats(queryClient),
        onError: (err) => {
          toast.error(err.message || "Failed to update pin");
        },
      },
    );
  };

  return (
    <button
      type="button"
      aria-label={pinned ? "Unpin session" : "Pin session"}
      title={pinned ? "Unpin session" : "Pin session"}
      disabled={setPinned.isPending}
      onClick={toggle}
      className={cn(
        "hover:bg-muted text-muted-foreground hover:text-foreground p-1 transition-all",
        pinned
          ? "text-foreground opacity-100"
          : "opacity-0 group-hover:opacity-100 focus-visible:opacity-100",
        setPinned.isPending && "opacity-50",
      )}
    >
      <Pin className={cn("size-4", pinned && "fill-current")} aria-hidden />
    </button>
  );
}

// Subtle copy button - always visible
function CopyButton({
  value,
  label,
  className,
}: {
  value: string;
  label: string;
  className?: string;
}) {
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(
    (e: React.MouseEvent) => {
      e.stopPropagation(); // Don't trigger row selection
      void navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    },
    [value],
  );

  return (
    <button
      type="button"
      onClick={handleCopy}
      className={cn(
        "cursor-pointer p-0.5 transition-colors",
        "opacity-50 hover:opacity-100",
        "hover:bg-muted/80",
        copied && "opacity-100",
        className,
      )}
      title={`Copy ${label}`}
      aria-label={`Copy ${label}`}
    >
      <Icon
        name={copied ? "check" : "copy"}
        className={cn(
          "size-3.5",
          copied ? "text-foreground" : "text-muted-foreground",
        )}
      />
    </button>
  );
}

export function ChatLogsTable({
  chats,
  selectedChatId,
  onSelectChat,
  onDeleteChat,
  isLoading,
  error,
  emptyState,
}: ChatLogsTableProps): JSX.Element {
  const { user } = useSession();
  const { data: membersData } = useMembers();

  // One batched lineage lookup for the visible rows. Optional decoration:
  // errors are swallowed (never blank the list) and the endpoint caps at 100
  // ids, matching the list's maximum page size.
  const chatIds = useMemo(
    () => chats.slice(0, 100).map((chat) => chat.id),
    [chats],
  );
  const { data: linksData } = useListChatSessionLinks({ chatIds }, undefined, {
    enabled: chatIds.length > 0,
    throwOnError: false,
    retry: false,
  });
  const lineageByChat = useMemo(() => {
    const map = new Map<string, LineageSummary>();
    const links = linksData?.links ?? [];
    for (const id of chatIds) {
      const summary = summarizeLineage(links, id);
      if (summary) map.set(id, summary);
    }
    return map;
  }, [chatIds, linksData]);
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null);
  if (isLoading && chats.length === 0) {
    return (
      <div className="flex h-64 items-center justify-center">
        <div className="flex flex-col items-center gap-3">
          <div className="border-muted-foreground/30 border-t-muted-foreground size-5 animate-spin rounded-full border-2" />
          <span className="text-muted-foreground text-sm">
            Loading traces...
          </span>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex h-64 items-center justify-center">
        <div className="flex flex-col items-center gap-3 px-4 text-center">
          <Icon name="triangle-alert" className="text-destructive size-5" />
          <div>
            <p className="text-foreground text-sm font-medium">
              Failed to load traces
            </p>
            <p className="text-muted-foreground mt-1 text-xs">
              {error.message}
            </p>
          </div>
        </div>
      </div>
    );
  }

  if (chats.length === 0) {
    return (
      <div className="flex h-64 items-center justify-center">
        <div className="flex flex-col items-center gap-3 px-4 text-center">
          <Icon name="inbox" className="text-muted-foreground size-5" />
          <div>
            <p className="text-foreground text-sm font-medium">
              {emptyState?.title ?? "No traces found"}
            </p>
            <p className="text-muted-foreground mt-1 text-xs">
              {emptyState?.description ??
                "Try adjusting your filters or time range"}
            </p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <>
      <div className="divide-border bg-card divide-y">
        {chats.map((chat) => {
          const isSelected = selectedChatId === chat.id;
          const source = chat.source;
          const riskCount = chat.riskFindingsCount ?? 0;
          const lastActivityTimestamp =
            chat.lastMessageTimestamp ?? chat.createdAt;

          return (
            <TableRowContextMenu
              key={chat.id}
              actions={[
                {
                  label: "Delete chat",
                  destructive: true,
                  onClick: () => setDeleteConfirmId(chat.id),
                },
              ]}
            >
              {/* Stretched select control under content; pin/delete/copy sit above
                  it (z-20) so interactive controls are never nested in a button. */}
              <div
                className={cn(
                  "group relative w-full px-5 py-4 transition-colors duration-150",
                  "hover:bg-muted/50",
                  isSelected && "bg-primary/5",
                )}
              >
                <button
                  type="button"
                  onClick={() => onSelectChat(chat)}
                  aria-label={`Open session ${getTraceId(chat.id)}`}
                  className="absolute inset-0 z-10 focus:outline-none"
                />
                <div className="pointer-events-none relative z-20 flex items-center gap-5">
                  {/* Left: Risk findings indicator */}
                  <div className="shrink-0">
                    <RiskIndicator count={riskCount} />
                  </div>

                  {/* Center: Main content */}
                  <div className="min-w-0 flex-1">
                    {/* Title — the scan target — with the lineage cluster */}
                    <div className="flex items-start gap-1.5">
                      <h3 className="text-foreground line-clamp-2 text-sm leading-snug font-medium">
                        {chat.title}
                      </h3>
                      <SessionLineageIcons
                        summary={lineageByChat.get(chat.id)}
                        onActivate={() => onSelectChat(chat)}
                      />
                    </div>

                    {/* Meta row — muted mono */}
                    <div className="text-muted-foreground mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-xs">
                      <span className="inline-flex items-center gap-1">
                        {getTraceId(chat.id)}
                        <span className="pointer-events-auto">
                          <CopyButton value={chat.id} label="Chat ID" />
                        </span>
                      </span>
                      <span className="text-muted-foreground/40">·</span>
                      <span className="inline-flex items-center gap-1.5">
                        {chat.assistantName ? (
                          <>
                            <Icon name="bot" className="size-3.5 opacity-60" />
                            <span className="max-w-[120px] truncate">
                              {chat.assistantName}
                            </span>
                          </>
                        ) : (
                          <>
                            <AccountTypeIcon accountType={chat.accountType} />
                            <span className="max-w-[120px] truncate">
                              <ChatOwnerLabel
                                members={membersData?.members}
                                chat={chat}
                                currentUser={user}
                                accountEmail={personalAccountEmail(chat)}
                              />
                            </span>
                          </>
                        )}
                      </span>
                      {source && (
                        <>
                          <span className="text-muted-foreground/40">·</span>
                          <span className="inline-flex items-center gap-1.5">
                            <HookSourceIcon
                              source={source}
                              className="size-3.5"
                            />
                            {formatChatSource(source, chat)}
                          </span>
                        </>
                      )}
                      <span className="text-muted-foreground/40">·</span>
                      <span>
                        Created {format(chat.createdAt, "MMM d, HH:mm")}
                      </span>
                      <span className="text-muted-foreground/40">·</span>
                      <span>
                        Last activity{" "}
                        {format(lastActivityTimestamp, "MMM d, HH:mm")}
                      </span>
                      <span className="text-muted-foreground/40">·</span>
                      <span className="tabular-nums">
                        {formatDuration(chat)}
                      </span>
                      <span className="text-muted-foreground/40">·</span>
                      <span className="tabular-nums">
                        {chat.numMessages} messages
                      </span>
                      {chat.totalCost !== undefined && chat.totalCost > 0 && (
                        <>
                          <span className="text-muted-foreground/40">·</span>
                          <span className="tabular-nums">
                            ${chat.totalCost.toFixed(4)}
                          </span>
                        </>
                      )}
                      <WorkUnitsRowMetrics chat={chat} />
                    </div>
                  </div>

                  {/* Right: Pin + Delete + Chevron */}
                  <div className="pointer-events-auto flex shrink-0 items-center gap-1">
                    <SessionPinButton
                      chatId={chat.id}
                      pinned={Boolean(chat.pinned)}
                    />
                    <button
                      type="button"
                      onClick={() => setDeleteConfirmId(chat.id)}
                      className="hover:bg-destructive/10 text-muted-foreground hover:text-destructive p-1 opacity-0 transition-all group-hover:opacity-100 focus-visible:opacity-100"
                      aria-label="Delete chat"
                    >
                      <Icon name="trash-2" className="size-4" />
                    </button>
                    <Icon
                      name="chevron-right"
                      className={cn(
                        "size-4 transition-colors",
                        isSelected
                          ? "text-foreground/60"
                          : "text-muted-foreground/40",
                      )}
                    />
                  </div>
                </div>
              </div>
            </TableRowContextMenu>
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
