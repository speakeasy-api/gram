import { Type } from "@/components/ui/type";
import { UpdatedAt } from "@/components/updated-at";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type { ChatMessage } from "@gram/client/models/components";
import { useListChats, useLoadChat } from "@gram/client/react-query";
import { Icon, Stack } from "@speakeasy-api/moonshine";
import { Loader2 } from "lucide-react";
import { useState } from "react";

const PREVIEW_LIMIT = 8;

/** Flatten a chat message's content (string | parts array | null) to text. */
function messageText(content: unknown): string {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content
      .map((part) => {
        if (typeof part === "string") return part;
        if (part && typeof part === "object" && "text" in part) {
          const text = (part as { text?: unknown }).text;
          return typeof text === "string" ? text : "";
        }
        return "";
      })
      .join(" ")
      .trim();
  }
  return "";
}

/**
 * The most recent sessions for an assistant, shown inline in the detail panel's
 * Sessions tab. Clicking a session expands its transcript in place rather than
 * navigating away; the full, filterable view still lives on the Agent Sessions
 * page via the per-session and footer links.
 */
export function AssistantSessionsList({
  assistantId,
}: {
  assistantId: string;
}): JSX.Element {
  const routes = useRoutes();
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const { data, isLoading } = useListChats(
    {
      assistantId,
      sortBy: "last_message_timestamp",
      sortOrder: "desc",
      limit: PREVIEW_LIMIT,
    },
    undefined,
    { retry: false, throwOnError: false },
  );

  const chats = data?.chats ?? [];

  if (isLoading) {
    return (
      <Stack align="center" justify="center" className="py-12">
        <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
      </Stack>
    );
  }

  if (chats.length === 0) {
    return (
      <Type small muted>
        No sessions yet. Conversations with this assistant will appear here.
      </Type>
    );
  }

  return (
    <Stack gap={2}>
      {chats.map((chat) => {
        const isExpanded = expandedId === chat.id;
        return (
          <div
            key={chat.id}
            className="border-border overflow-hidden rounded-md border"
          >
            <button
              type="button"
              onClick={() => setExpandedId(isExpanded ? null : chat.id)}
              className="hover:bg-surface-secondary flex w-full items-center justify-between gap-3 px-3 py-2 text-left transition-colors"
            >
              <Stack gap={0} className="min-w-0">
                <Type small className="truncate font-medium">
                  {chat.title || "Untitled session"}
                </Type>
                <Type muted className="text-[11px]">
                  {chat.numMessages}{" "}
                  {chat.numMessages === 1 ? "message" : "messages"}
                </Type>
              </Stack>
              <div className="flex shrink-0 items-center gap-2">
                <span className="text-muted-foreground text-[11px]">
                  <UpdatedAt date={new Date(chat.updatedAt)} />
                </span>
                <Icon
                  name={isExpanded ? "chevron-down" : "chevron-right"}
                  className="text-muted-foreground h-3 w-3"
                />
              </div>
            </button>
            {isExpanded && (
              <SessionTranscript chatId={chat.id} assistantId={assistantId} />
            )}
          </div>
        );
      })}

      {(data?.total ?? chats.length) > chats.length && (
        <routes.agentSessions.Link
          queryParams={{ assistantId }}
          className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 self-start rounded-md px-1 py-1 text-xs no-underline transition-colors hover:no-underline"
        >
          View all sessions
          <Icon name="chevron-right" className="h-3 w-3" />
        </routes.agentSessions.Link>
      )}
    </Stack>
  );
}

function SessionTranscript({
  chatId,
  assistantId,
}: {
  chatId: string;
  assistantId: string;
}): JSX.Element {
  const routes = useRoutes();
  const { data, isLoading } = useLoadChat({ id: chatId }, undefined, {
    retry: false,
    throwOnError: false,
  });

  const messages = (data?.messages ?? []).filter((m) => m.role !== "system");

  return (
    <div className="border-border bg-muted/20 border-t">
      {isLoading ? (
        <Stack align="center" justify="center" className="py-6">
          <Loader2 className="text-muted-foreground h-4 w-4 animate-spin" />
        </Stack>
      ) : messages.length === 0 ? (
        <Type muted small className="px-3 py-3">
          No messages in this session.
        </Type>
      ) : (
        <div className="max-h-72 space-y-2.5 overflow-y-auto px-3 py-2.5">
          {messages.map((message, i) => (
            <TranscriptMessage key={i} message={message} />
          ))}
        </div>
      )}
      <div className="border-border border-t px-3 py-1.5">
        <routes.agentSessions.Link
          queryParams={{ assistantId, chatId }}
          className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 rounded-md text-[11px] no-underline transition-colors hover:no-underline"
        >
          Open full session
          <Icon name="chevron-right" className="h-3 w-3" />
        </routes.agentSessions.Link>
      </div>
    </div>
  );
}

function TranscriptMessage({ message }: { message: ChatMessage }): JSX.Element {
  const text = messageText(message.content);
  const isTool = message.role === "tool" || !!message.toolCalls;
  const body = text || (isTool ? "(tool call)" : "");

  return (
    <div>
      <Type
        className={cn(
          "text-[10px] font-medium tracking-wide uppercase",
          message.role === "assistant"
            ? "text-primary/70"
            : "text-muted-foreground",
        )}
      >
        {message.role}
      </Type>
      <Type small className="break-words whitespace-pre-wrap">
        {body}
      </Type>
    </div>
  );
}
