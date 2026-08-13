import { useChatDeleteMutation } from "@gram/client/react-query/chatDelete.js";
import { invalidateAllListChats } from "@gram/client/react-query/listChats.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ChatDetailSheet } from "./ChatDetailPanel";

/**
 * Owns the selected-chat state and default delete wiring for a
 * ChatDetailSheet. Call `openChat(id)` from any row; render `sheet` once at
 * the end of the component.
 */
export function useChatDetailSheet(): {
  selectedChatId: string | null;
  openChat: (chatId: string) => void;
  sheet: JSX.Element;
} {
  const queryClient = useQueryClient();
  const deleteChat = useChatDeleteMutation();
  const [selectedChatId, setSelectedChatId] = useState<string | null>(null);

  const sheet = (
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
  );

  return { selectedChatId, openChat: setSelectedChatId, sheet };
}
