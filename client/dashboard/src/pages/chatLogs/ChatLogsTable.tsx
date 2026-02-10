import type { ChatOverviewWithResolutions } from "@gram/client/models/components";
import { cn } from "@/lib/utils";
import { Icon } from "@speakeasy-api/moonshine";
import { format } from "date-fns";

interface ChatLogsTableProps {
  chats: ChatOverviewWithResolutions[];
  selectedChatId?: string;
  onSelectChat: (chat: ChatOverviewWithResolutions) => void;
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
  const seconds = Math.round(
    (chat.updatedAt.getTime() - chat.createdAt.getTime()) / 1000,
  );
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  return remainingSeconds > 0
    ? `${minutes}m ${remainingSeconds}s`
    : `${minutes}m`;
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
    success: "stroke-emerald-500",
    failure: "stroke-rose-500",
    partial: "stroke-amber-500",
    unresolved: "stroke-muted-foreground/30",
  };

  const bgColorMap = {
    success: "stroke-emerald-500/15",
    failure: "stroke-rose-500/15",
    partial: "stroke-amber-500/15",
    unresolved: "stroke-muted-foreground/10",
  };

  return (
    <div className="flex flex-col items-center gap-1">
      <div className="relative" style={{ width: size, height: size }}>
        <svg className="transform -rotate-90" width={size} height={size}>
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
      <span className="text-[9px] uppercase tracking-wider text-muted-foreground font-medium">
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
    success: "bg-emerald-500",
    failure: "bg-rose-500",
    partial: "bg-amber-500",
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
  isLoading,
  error,
}: ChatLogsTableProps) {
  if (isLoading && chats.length === 0) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="flex flex-col items-center gap-3">
          <div className="size-5 border-2 border-muted-foreground/30 border-t-muted-foreground rounded-full animate-spin" />
          <span className="text-sm text-muted-foreground">
            Loading traces...
          </span>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="flex flex-col items-center gap-3 text-center px-4">
          <div className="size-10 rounded-full bg-rose-500/10 flex items-center justify-center">
            <Icon name="triangle-alert" className="size-5 text-rose-500" />
          </div>
          <div>
            <p className="text-sm font-medium text-foreground">
              Failed to load traces
            </p>
            <p className="text-xs text-muted-foreground mt-1">
              {error.message}
            </p>
          </div>
        </div>
      </div>
    );
  }

  if (chats.length === 0) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="flex flex-col items-center gap-3 text-center px-4">
          <div className="size-10 rounded-full bg-muted flex items-center justify-center">
            <Icon name="inbox" className="size-5 text-muted-foreground" />
          </div>
          <div>
            <p className="text-sm font-medium text-foreground">
              No traces found
            </p>
            <p className="text-xs text-muted-foreground mt-1">
              Try adjusting your filters or time range
            </p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="divide-y divide-border/50">
      {chats.map((chat) => {
        const status = getOverallResolutionStatus(chat.resolutions);
        const averageScore = getAverageScore(chat.resolutions);
        const isSelected = selectedChatId === chat.id;
        const hasResolutions = chat.resolutions.length > 0;

        return (
          <button
            key={chat.id}
            onClick={() => onSelectChat(chat)}
            className={cn(
              "w-full text-left px-5 py-4 transition-all duration-150",
              "hover:bg-muted/50",
              "focus:outline-none focus-visible:bg-muted/50",
              isSelected && "bg-primary/[0.03] hover:bg-primary/[0.05]",
            )}
          >
            <div className="flex items-center gap-5">
              {/* Left: Score ring or status indicator */}
              <div className="shrink-0">
                {hasResolutions ? (
                  <ScoreRing score={averageScore} status={status} size={44} />
                ) : (
                  <div className="flex flex-col items-center gap-1">
                    <div className="size-[44px] rounded-full bg-muted/50 flex items-center justify-center">
                      <Icon
                        name="message-circle"
                        className="size-5 text-muted-foreground/60"
                      />
                    </div>
                    <span className="text-[9px] uppercase tracking-wider text-muted-foreground/50 font-medium">
                      &nbsp;
                    </span>
                  </div>
                )}
              </div>

              {/* Center: Main content */}
              <div className="flex-1 min-w-0">
                {/* Header row */}
                <div className="flex items-center gap-2 mb-1.5">
                  <StatusDot status={status} />
                  <span className="text-xs font-semibold text-muted-foreground tracking-wide uppercase">
                    {getTraceId(chat.id)}
                  </span>
                  <span className="text-muted-foreground/40">·</span>
                  <span className="text-sm text-muted-foreground">
                    {format(chat.createdAt, "MMM d, HH:mm")}
                  </span>
                </div>

                {/* Title */}
                <h3 className="text-sm font-medium text-foreground leading-snug line-clamp-2 mb-2">
                  {chat.title}
                </h3>

                {/* Metadata row */}
                <div className="flex items-center gap-4 text-sm text-muted-foreground">
                  <span className="flex items-center gap-1.5">
                    <Icon name="user" className="size-4 opacity-60" />
                    <span className="truncate max-w-[120px]">
                      {chat.externalUserId || "anonymous"}
                    </span>
                  </span>
                  <span className="flex items-center gap-1.5">
                    <Icon name="timer" className="size-4 opacity-60" />
                    {formatDuration(chat)}
                  </span>
                  <span className="flex items-center gap-1.5">
                    <Icon name="message-square" className="size-4 opacity-60" />
                    {chat.numMessages} messages
                  </span>
                </div>
              </div>

              {/* Right: Chevron */}
              <div className="shrink-0 pt-2">
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
  );
}
