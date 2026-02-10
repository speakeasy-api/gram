import type { ChatOverviewWithResolutions } from "@gram/client/models/components";
import { cn } from "@/lib/utils";
import { Icon, Badge } from "@speakeasy-api/moonshine";
import { ResolutionBadges } from "./ResolutionBadges";

interface ChatLogsTableProps {
  chats: ChatOverviewWithResolutions[];
  selectedChatId?: string;
  onSelectChat: (chat: ChatOverviewWithResolutions) => void;
  isLoading: boolean;
  error: Error | null;
}

function getTraceId(chatId: string): string {
  return `trace-${chatId.slice(0, 3)}`;
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

function getDuration(chat: ChatOverviewWithResolutions): string {
  const duration = Math.round(
    (chat.updatedAt.getTime() - chat.createdAt.getTime()) / 1000,
  );
  return `${duration}s`;
}

function getContextQuality(score: number): {
  label: string;
  variant: "success" | "warning" | "destructive";
} {
  if (score >= 80) return { label: "Good Context", variant: "success" };
  if (score >= 50) return { label: "Fair Context", variant: "warning" };
  return { label: "Poor Context", variant: "destructive" };
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
      <div className="p-8 text-center text-muted-foreground">Loading...</div>
    );
  }

  if (error) {
    return (
      <div className="p-8 text-center text-destructive">
        Error loading chats: {error.message}
      </div>
    );
  }

  if (chats.length === 0) {
    return (
      <div className="p-8 text-center text-muted-foreground">
        No chats found. Try adjusting your filters.
      </div>
    );
  }

  return (
    <div>
      {chats.map((chat) => {
        const status = getOverallResolutionStatus(chat.resolutions);
        const averageScore = getAverageScore(chat.resolutions);
        const contextQuality = getContextQuality(averageScore);
        const isSelected = selectedChatId === chat.id;

        return (
          <button
            key={chat.id}
            onClick={() => onSelectChat(chat)}
            className={cn(
              "w-full text-left p-4 transition-colors relative",
              "hover:bg-muted/30",
              isSelected &&
                "bg-primary/5 before:absolute before:left-0 before:top-0 before:bottom-0 before:w-1 before:bg-primary",
            )}
          >
            <div className="flex items-start gap-3 mb-2">
              <span className="font-mono text-sm font-medium">
                {getTraceId(chat.id)}
              </span>
              {status === "success" && (
                <Icon name="circle-check" className="size-4 text-success" />
              )}
              {status === "failure" && (
                <Icon name="circle-x" className="size-4 text-destructive" />
              )}
              {status === "unresolved" && (
                <Icon name="circle" className="size-4 text-muted-foreground" />
              )}
              <Icon
                name="chevron-right"
                className="size-4 text-muted-foreground ml-auto"
              />
            </div>

            <div className="text-sm mb-3">{chat.title}</div>

            <div className="flex items-center gap-3 text-xs text-muted-foreground mb-3">
              <span className="flex items-center gap-1">
                <Icon name="user" className="size-3" />
                {chat.externalUserId || "anonymous"}
              </span>
              <span className="flex items-center gap-1">
                <Icon name="clock" className="size-3" />
                {getDuration(chat)}
              </span>
              <span className="flex items-center gap-1">
                <Icon name="zap" className="size-3" />
                {chat.numMessages}
              </span>
            </div>

            {chat.resolutions.length > 0 && (
              <>
                <div className="mb-2">
                  <div className="flex items-center justify-between text-xs mb-1">
                    <div className="h-2 flex-1 bg-muted rounded-full overflow-hidden">
                      <div
                        className={cn(
                          "h-full transition-all",
                          status === "success" && "bg-success",
                          status === "failure" && "bg-danger",
                          status === "partial" && "bg-warning",
                        )}
                        style={{ width: `${averageScore}%` }}
                      />
                    </div>
                    <span className="ml-2 font-medium">{averageScore}%</span>
                  </div>
                </div>

                <div className="flex items-center gap-2">
                  <Badge variant={contextQuality.variant}>
                    <Icon name="circle-check" className="size-3" />
                    {contextQuality.label}
                  </Badge>
                  <div className="ml-auto">
                    <ResolutionBadges resolutions={chat.resolutions} />
                  </div>
                </div>
              </>
            )}
          </button>
        );
      })}
    </div>
  );
}
