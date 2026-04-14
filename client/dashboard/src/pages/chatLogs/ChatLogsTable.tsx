import { Dialog } from "@/components/ui/dialog";
import { SimpleTooltip } from "@/components/ui/tooltip";
import {
  resolutionBgColors,
  resolutionStrokeColors,
  resolutionStrokeMutedColors,
} from "@/lib/resolution-colors";
import { cn } from "@/lib/utils";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import type { ChatOverviewWithResolutions } from "@gram/client/models/components";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { format } from "date-fns";
import { useCallback, useState } from "react";

interface ChatLogsTableProps {
  chats: ChatOverviewWithResolutions[];
  selectedChatId?: string;
  onSelectChat: (chat: ChatOverviewWithResolutions) => void;
  onDeleteChat: (chatId: string) => void;
  isLoading: boolean;
  error: Error | null;
}

function getTraceId(chatId: string): string {
  return chatId.slice(0, 8);
}

function getOverallResolutionStatus(
  resolutions: ChatOverviewWithResolutions["resolutions"],
): "success" | "failure" | "partial" | "unresolved" {
  if (resolutions.length === 0) return "unresolved";

  const hasFailure = resolutions.some((r) => r.resolution === "failure");
  const hasSuccess = resolutions.some((r) => r.resolution === "success");

  if (hasFailure) return "failure";
  if (hasSuccess) return "success";
  return "partial";
}

function getAverageScore(
  resolutions: ChatOverviewWithResolutions["resolutions"],
): number {
  if (resolutions.length === 0) return 0;
  const sum = resolutions.reduce((acc, r) => acc + r.score, 0);
  return Math.round(sum / resolutions.length);
}

function formatDuration(chat: ChatOverviewWithResolutions): string {
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
      // Copy with the label prefix
      navigator.clipboard.writeText(`${label}: ${value}`);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    },
    [value, label],
  );

  return (
    <button
      onClick={handleCopy}
      className={cn(
        "rounded p-0.5 transition-colors",
        "opacity-50 hover:opacity-100",
        "hover:bg-muted/80",
        copied && "opacity-100",
        className,
      )}
      title={`Copy ${label}`}
    >
      <Icon
        name={copied ? "check" : "copy"}
        className={cn(
          "size-3.5",
          copied ? "text-emerald-500" : "text-muted-foreground",
        )}
      />
    </button>
  );
}

// Circular progress indicator component with label
function ScoreRing({
  score,
  status,
  size = 44,
}: {
  score: number;
  status: "success" | "failure" | "partial" | "unresolved";
  size?: number;
}) {
  const strokeWidth = 3;
  const radius = (size - strokeWidth) / 2;
  const circumference = radius * 2 * Math.PI;
  const offset = circumference - (score / 100) * circumference;

  const colorMap = {
    ...resolutionStrokeColors,
    unresolved: "stroke-muted-foreground/30",
  };

  const bgColorMap = {
    ...resolutionStrokeMutedColors,
    unresolved: "stroke-muted-foreground/10",
  };

  return (
    <div className="flex flex-col items-center gap-1">
      <div className="relative" style={{ width: size, height: size }}>
        <svg className="-rotate-90 transform" width={size} height={size}>
          {/* Background circle */}
          <circle
            cx={size / 2}
            cy={size / 2}
            r={radius}
            strokeWidth={strokeWidth}
            fill="none"
            className={bgColorMap[status]}
          />
          {/* Progress circle */}
          <circle
            cx={size / 2}
            cy={size / 2}
            r={radius}
            strokeWidth={strokeWidth}
            fill="none"
            strokeLinecap="round"
            className={cn(colorMap[status], "transition-all duration-500")}
            style={{
              strokeDasharray: circumference,
              strokeDashoffset: offset,
            }}
          />
        </svg>
        <div className="absolute inset-0 flex items-center justify-center">
          <span className="text-xs font-semibold tabular-nums">{score}</span>
        </div>
      </div>
      <span className="text-muted-foreground text-[9px] font-medium tracking-wider uppercase">
        Score
      </span>
    </div>
  );
}

// Status indicator dot
function StatusDot({
  status,
}: {
  status: "success" | "failure" | "partial" | "unresolved";
}) {
  const colorMap = {
    ...resolutionBgColors,
    unresolved: "bg-muted-foreground/40",
  };

  return (
    <span
      className={cn("inline-flex h-2 w-2 rounded-full", colorMap[status])}
    />
  );
}

export function ChatLogsTable({
  chats,
  selectedChatId,
  onSelectChat,
  onDeleteChat,
  isLoading,
  error,
}: ChatLogsTableProps) {
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
          <div className="flex size-10 items-center justify-center rounded-full bg-rose-500/10">
            <Icon name="triangle-alert" className="size-5 text-rose-500" />
          </div>
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
          <div className="bg-muted flex size-10 items-center justify-center rounded-full">
            <Icon name="inbox" className="text-muted-foreground size-5" />
          </div>
          <div>
            <p className="text-foreground text-sm font-medium">
              No traces found
            </p>
            <p className="text-muted-foreground mt-1 text-xs">
              Try adjusting your filters or time range
            </p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <>
      <div className="divide-border/50 divide-y">
        {chats.map((chat) => {
          const status = getOverallResolutionStatus(chat.resolutions);
          const averageScore = getAverageScore(chat.resolutions);
          const isSelected = selectedChatId === chat.id;
          const hasResolutions = chat.resolutions.length > 0;
          const source = chat.source;

          return (
            <button
              key={chat.id}
              onClick={() => onSelectChat(chat)}
              className={cn(
                "group w-full px-5 py-4 text-left transition-all duration-150",
                "hover:bg-muted/50",
                "focus-visible:bg-muted/50 focus:outline-none",
                isSelected && "bg-primary/[0.03] hover:bg-primary/[0.05]",
              )}
            >
              <div className="flex items-center gap-5">
                {/* Left: Score ring or N/A indicator */}
                <div className="shrink-0">
                  {hasResolutions ? (
                    <ScoreRing score={averageScore} status={status} size={44} />
                  ) : (
                    <SimpleTooltip tooltip="This session hasn't been analyzed yet. Scores are generated automatically after a conversation ends.">
                      <div className="flex flex-col items-center gap-1">
                        <div className="border-muted-foreground/30 flex size-[44px] items-center justify-center rounded-full border-[3px]">
                          <span className="text-muted-foreground text-[10px] font-semibold">
                            N/A
                          </span>
                        </div>
                        <span className="text-muted-foreground text-[9px] font-medium tracking-wider uppercase">
                          Score
                        </span>
                      </div>
                    </SimpleTooltip>
                  )}
                </div>

                {/* Center: Main content */}
                <div className="min-w-0 flex-1">
                  {/* Header row */}
                  <div className="mb-1.5 flex items-center gap-2">
                    <StatusDot status={status} />
                    <span className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
                      {getTraceId(chat.id)}
                    </span>
                    <CopyButton value={chat.id} label="Chat ID" />
                    <span className="text-muted-foreground/40">·</span>
                    <span className="text-muted-foreground text-sm">
                      {format(chat.createdAt, "MMM d, HH:mm")}
                    </span>
                  </div>

                  {/* Title */}
                  <h3 className="text-foreground mb-2 line-clamp-2 text-sm leading-snug font-medium">
                    {chat.title}
                  </h3>

                  {/* Metadata row */}
                  <div className="text-muted-foreground flex items-center gap-4 text-sm">
                    <span className="flex items-center gap-1.5">
                      <Icon name="user" className="size-4 opacity-60" />
                      <span className="max-w-[120px] truncate">
                        {chat.externalUserId || "anonymous"}
                      </span>
                    </span>
                    {source && (
                      <span className="flex items-center gap-1.5">
                        <HookSourceIcon source={source} className="size-4" />
                        {source}
                      </span>
                    )}
                    <span className="flex items-center gap-1.5">
                      <Icon name="timer" className="size-4 opacity-60" />
                      {formatDuration(chat)}
                    </span>
                    <span className="flex items-center gap-1.5">
                      <Icon
                        name="message-square"
                        className="size-4 opacity-60"
                      />
                      {chat.numMessages} messages
                    </span>
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
                    className="hover:bg-destructive/10 text-muted-foreground hover:text-destructive rounded-md p-1 opacity-0 transition-all group-hover:opacity-100"
                    aria-label="Delete chat"
                  >
                    <Icon name="trash-2" className="size-4" />
                  </span>
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
            </button>
          );
        })}
      </div>

      <Dialog
        open={deleteConfirmId !== null}
        onOpenChange={(open) => !open && setDeleteConfirmId(null)}
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
