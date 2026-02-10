import type { ChatOverviewWithResolutions } from "@gram/client/models/components";
import { useListChatsWithResolutions } from "@gram/client/react-query";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { ChatDetailPanel } from "./ChatDetailPanel";
import { ChatLogsFilters } from "./ChatLogsFilters";
import { ChatLogsTable } from "./ChatLogsTable";

export default function ChatLogs() {
  const [selectedChat, setSelectedChat] =
    useState<ChatOverviewWithResolutions | null>(null);
  const [externalCustomerId, setExternalCustomerId] = useState<string>("");
  const [resolutionStatus, setResolutionStatus] = useState<string>("");

  const [offset, setOffset] = useState(0);
  const limit = 50;

  const { data, isLoading, error } = useListChatsWithResolutions({
    externalUserId: externalCustomerId || undefined,
    resolutionStatus: resolutionStatus || undefined,
    limit,
    offset,
  });

  const chats = data?.chats ?? [];
  const hasMore = chats.length === limit;

  return (
    <div className="flex h-full">
      {/* Left side: List view */}
      <div className="flex-1 flex flex-col border-r">
        <div className="p-6 border-b">
          <h1 className="text-2xl font-semibold mb-1">Chat Traces</h1>
          <p className="text-sm text-muted-foreground mb-4">
            View and debug individual chat conversations
          </p>
          <ChatLogsFilters
            externalCustomerId={externalCustomerId}
            onExternalCustomerIdChange={setExternalCustomerId}
            resolutionStatus={resolutionStatus}
            onResolutionStatusChange={setResolutionStatus}
          />
        </div>

        <div className="flex-1 overflow-y-auto">
          <ChatLogsTable
            chats={chats}
            selectedChatId={selectedChat?.id}
            onSelectChat={setSelectedChat}
            isLoading={isLoading}
            error={error}
          />

          {hasMore && (
            <div className="p-4 flex justify-center gap-2">
              <Button
                onClick={() => setOffset(Math.max(0, offset - limit))}
                disabled={offset === 0}
              >
                Previous
              </Button>
              <Button onClick={() => setOffset(offset + limit)}>Next</Button>
            </div>
          )}
        </div>
      </div>

      {/* Right side: Detail panel */}
      <div className="w-1/2">
        {selectedChat ? (
          <ChatDetailPanel
            chatId={selectedChat.id}
            resolutions={selectedChat.resolutions}
            onClose={() => setSelectedChat(null)}
          />
        ) : (
          <div className="h-full flex items-center justify-center text-muted-foreground">
            <Stack direction="vertical" gap={2} align="center">
              <Icon name="messages-square" className="size-12 opacity-50" />
              <p>Select a chat to view details</p>
            </Stack>
          </div>
        )}
      </div>
    </div>
  );
}
