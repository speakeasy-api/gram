import { Type } from "@/components/ui/type";
import { formatPlatform } from "@/lib/formatPlatform";
import { cn } from "@/lib/utils";
import { ChatDetailSheet } from "@/pages/chatLogs/ChatDetailPanel";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import { useRoutes } from "@/routes";
import { useChatDeleteMutation } from "@gram/client/react-query/chatDelete.js";
import {
  invalidateAllListChats,
  useListChats,
} from "@gram/client/react-query/listChats.js";
import { Icon, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { format } from "date-fns";
import { Loader2 } from "lucide-react";
import { useState } from "react";

const PREVIEW_LIMIT = 8;

/**
 * A miniaturised version of the Agent Sessions list for the assistant detail
 * panel's Sessions tab: the same row shape (risk indicator, title, activity,
 * source) at a compact scale. Selecting a session opens the same
 * ChatDetailSheet the Agent Sessions page uses — an overlay, not a navigation —
 * so the detail view is identical. The footer links to the full, filterable
 * page.
 */
export function AssistantSessionsList({
  assistantId,
}: {
  assistantId: string;
}): JSX.Element {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const deleteChat = useChatDeleteMutation();
  const [selectedChatId, setSelectedChatId] = useState<string | null>(null);

  const { data, isLoading, error } = useListChats(
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

  if (error) {
    return (
      <Type small muted>
        Couldn't load sessions. {error.message}
      </Type>
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
    <>
      <Stack gap={2}>
        <div className="divide-border/60 overflow-hidden rounded-md border divide-y">
          {chats.map((chat) => {
            const isSelected = selectedChatId === chat.id;
            const lastActivity = chat.lastMessageTimestamp ?? chat.createdAt;
            return (
              <button
                key={chat.id}
                type="button"
                onClick={() => setSelectedChatId(chat.id)}
                className={cn(
                  "hover:bg-muted/50 block w-full px-3 py-2.5 text-left transition-colors",
                  isSelected && "bg-primary/5",
                )}
              >
                <Type small className="line-clamp-2 font-medium">
                  {chat.title || "Untitled session"}
                </Type>
                <div className="text-muted-foreground mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px]">
                  <span>
                    {chat.numMessages}{" "}
                    {chat.numMessages === 1 ? "message" : "messages"}
                  </span>
                  <span className="text-muted-foreground/40">·</span>
                  <span>{format(new Date(lastActivity), "MMM d, HH:mm")}</span>
                  {chat.source && (
                    <>
                      <span className="text-muted-foreground/40">·</span>
                      <span className="inline-flex items-center gap-1">
                        <HookSourceIcon
                          source={chat.source}
                          className="size-3"
                        />
                        {formatPlatform(chat.source)}
                      </span>
                    </>
                  )}
                </div>
              </button>
            );
          })}
        </div>

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

      <ChatDetailSheet
        chatId={selectedChatId}
        onClose={() => setSelectedChatId(null)}
        onDelete={(chatId) => {
          deleteChat.mutate(
            { request: { id: chatId } },
            {
              onSuccess: () => {
                void invalidateAllListChats(queryClient);
                setSelectedChatId(null);
              },
            },
          );
        }}
      />
    </>
  );
}
