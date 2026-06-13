import { Type } from "@/components/ui/type";
import { UpdatedAt } from "@/components/updated-at";
import { useRoutes } from "@/routes";
import { useListChats } from "@gram/client/react-query";
import { Icon, Stack } from "@speakeasy-api/moonshine";
import { Loader2 } from "lucide-react";

const PREVIEW_LIMIT = 8;

/**
 * A compact, read-mostly list of an assistant's most recent sessions for the
 * detail panel's Sessions tab. The full, filterable table lives on the Agent
 * Sessions page; each row and the footer link deep-link into it.
 */
export function AssistantSessionsList({
  assistantId,
}: {
  assistantId: string;
}): JSX.Element {
  const routes = useRoutes();

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
      {chats.map((chat) => (
        <routes.agentSessions.Link
          key={chat.id}
          queryParams={{ assistantId, chatId: chat.id }}
          className="border-border hover:bg-surface-secondary flex items-center justify-between gap-3 rounded-md border px-3 py-2 transition-colors hover:no-underline"
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
          <span className="text-muted-foreground shrink-0 text-[11px]">
            <UpdatedAt date={new Date(chat.updatedAt)} />
          </span>
        </routes.agentSessions.Link>
      ))}

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
