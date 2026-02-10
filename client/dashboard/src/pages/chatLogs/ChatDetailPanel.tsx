import { cn } from "@/lib/utils";
import type { ChatResolution } from "@gram/client/models/components";
import { useLoadChat } from "@gram/client/react-query";
import { Badge, Icon, Stack } from "@speakeasy-api/moonshine";
import { format } from "date-fns";
import { CircularProgress } from "./CircularProgress";

interface ChatDetailPanelProps {
  chatId: string;
  resolutions: ChatResolution[];
  onClose: () => void;
}

function getTraceId(chatId: string): string {
  return `trace-${chatId.slice(0, 3)}`;
}

function getOverallResolutionStatus(
  resolutions: ChatResolution[],
): "success" | "failure" | "partial" | "unresolved" {
  if (resolutions.length === 0) return "unresolved";

  const hasFailure = resolutions.some((r) => r.resolution === "failure");
  const hasSuccess = resolutions.some((r) => r.resolution === "success");

  if (hasFailure) return "failure";
  if (hasSuccess) return "success";
  return "partial";
}

function getAverageScore(resolutions: ChatResolution[]): number {
  if (resolutions.length === 0) return 0;
  const sum = resolutions.reduce((acc, r) => acc + r.score, 0);
  return Math.round(sum / resolutions.length);
}

function getContextQuality(score: number): {
  label: string;
  variant: "success" | "warning" | "destructive";
} {
  if (score >= 80) return { label: "Good Context", variant: "success" };
  if (score >= 50) return { label: "Fair Context", variant: "warning" };
  return { label: "Poor Context", variant: "destructive" };
}

export function ChatDetailPanel({
  chatId,
  resolutions,
  onClose: _onClose,
}: ChatDetailPanelProps) {
  const { data: chat, isLoading } = useLoadChat({ id: chatId }, undefined, {});

  if (isLoading) {
    return <div className="p-8">Loading chat details...</div>;
  }

  if (!chat) {
    return <div className="p-8">Chat not found</div>;
  }

  const status = getOverallResolutionStatus(resolutions);
  const averageScore = getAverageScore(resolutions);
  const contextQuality = getContextQuality(averageScore);
  const duration = Math.round(
    (new Date(chat.updatedAt).getTime() - new Date(chat.createdAt).getTime()) /
      1000,
  );

  // Count tool calls (messages with tool role)
  const toolCalls = chat.messages.filter((m) => m.role === "tool").length;

  // Create a map of message IDs to resolution info for showing breakpoints
  const messageResolutionMap = new Map<string, ChatResolution>();
  resolutions.forEach((res) => {
    res.messageIds.forEach((msgId) => {
      messageResolutionMap.set(msgId, res);
    });
  });

  return (
    <div className="h-full flex flex-col bg-background">
      {/* Header */}
      <div className="p-6 border-b">
        <div className="flex items-center justify-between mb-2">
          <h2 className="text-xl font-semibold">{getTraceId(chatId)}</h2>
          {status !== "unresolved" && (
            <Badge
              variant={
                status === "success"
                  ? "success"
                  : status === "failure"
                    ? "destructive"
                    : "warning"
              }
            >
              <Icon name="circle-check" className="size-3" />
              {status === "success"
                ? "Resolved"
                : status === "failure"
                  ? "Failed"
                  : "Partial"}
            </Badge>
          )}
        </div>
        <div className="text-sm text-muted-foreground mb-3">
          {format(new Date(chat.createdAt), "yyyy-MM-dd HH:mm:ss")}
        </div>
        <div className="text-sm">{chat.title}</div>
      </div>

      {/* Metadata Grid */}
      <div className="p-6 border-b bg-muted/10">
        <div className="grid grid-cols-2 gap-x-8 gap-y-4">
          <div>
            <div className="text-xs text-muted-foreground mb-1">User ID:</div>
            <div className="text-sm font-medium">
              {chat.externalUserId || "anonymous"}
            </div>
          </div>
          <div>
            <div className="text-xs text-muted-foreground mb-1">Duration:</div>
            <div className="text-sm font-medium">{duration}s</div>
          </div>
          <div>
            <div className="text-xs text-muted-foreground mb-1">Messages:</div>
            <div className="text-sm font-medium">{chat.messages.length}</div>
          </div>
          <div>
            <div className="text-xs text-muted-foreground mb-1">
              Tool Calls:
            </div>
            <div className="text-sm font-medium">{toolCalls}</div>
          </div>
          {resolutions.length > 0 && (
            <>
              <div>
                <div className="text-xs text-muted-foreground mb-1">
                  Resolution Score:
                </div>
                <div className="text-sm font-medium">{averageScore}%</div>
              </div>
              <div>
                <div className="text-xs text-muted-foreground mb-1">
                  Context Quality:
                </div>
                <Badge variant={contextQuality.variant}>
                  <Icon name="circle-check" className="size-3" />
                  {contextQuality.label}
                </Badge>
              </div>
            </>
          )}
        </div>
      </div>

      {/* Resolutions Summary */}
      {resolutions.length > 0 && (
        <div className="p-6 border-b">
          <Stack direction="vertical" gap={3}>
            {resolutions.map((resolution) => (
              <div key={resolution.id} className="flex items-start gap-4">
                <CircularProgress
                  score={resolution.score}
                  status={
                    resolution.resolution as "success" | "failure" | "partial"
                  }
                  size="sm"
                />
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-medium mb-1">
                    {resolution.userGoal}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {resolution.resolutionNotes}
                  </div>
                </div>
              </div>
            ))}
          </Stack>
        </div>
      )}

      {/* Chat Messages */}
      <div className="flex-1 overflow-y-auto p-6">
        <Stack direction="vertical" gap={4}>
          {chat.messages.map((message) => {
            const resolution = messageResolutionMap.get(message.id);

            return (
              <div key={message.id}>
                {/* Resolution breakpoint */}
                {resolution && (
                  <div className="mb-3 p-3 rounded-lg bg-primary/10 border-l-4 border-primary">
                    <div className="text-xs font-semibold">
                      Resolution Point: {resolution.resolution}
                    </div>
                  </div>
                )}

                {/* Message */}
                <div className="flex items-start gap-3">
                  {message.role === "user" && (
                    <div className="size-8 rounded-full bg-primary/10 flex items-center justify-center flex-shrink-0">
                      <Icon name="user" className="size-4 text-primary" />
                    </div>
                  )}
                  {message.role === "assistant" && (
                    <div className="size-8 rounded-full bg-muted flex items-center justify-center flex-shrink-0">
                      <Icon name="bot" className="size-4" />
                    </div>
                  )}
                  {message.role === "tool" && (
                    <div className="size-8 rounded-full bg-primary flex items-center justify-center flex-shrink-0">
                      <Icon
                        name="zap"
                        className="size-4 text-primary-foreground"
                      />
                    </div>
                  )}

                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1">
                      <span className="text-sm font-semibold capitalize">
                        {message.role}
                      </span>
                      <span className="text-xs text-muted-foreground">
                        {message.createdAt &&
                          format(new Date(message.createdAt), "HH:mm:ss")}
                      </span>
                    </div>
                    <div
                      className={cn(
                        "p-3 rounded-lg text-sm",
                        message.role === "user" && "bg-primary/5",
                        message.role === "assistant" && "bg-muted/50",
                        message.role === "tool" && "bg-background border",
                      )}
                    >
                      {message.role === "tool" &&
                      typeof message.content === "object" &&
                      message.content !== null ? (
                        <div>
                          <div className="text-xs font-semibold mb-2">
                            Parameters:
                          </div>
                          <pre className="text-xs overflow-x-auto">
                            {JSON.stringify(message.content, null, 2)}
                          </pre>
                        </div>
                      ) : (
                        <div className="whitespace-pre-wrap">
                          {typeof message.content === "string"
                            ? message.content
                            : JSON.stringify(message.content)}
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              </div>
            );
          })}
        </Stack>
      </div>
    </div>
  );
}
