import { useChatDeleteMutation } from "@gram/client/react-query/chatDelete.js";
import { invalidateAllListChats } from "@gram/client/react-query/listChats.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { type To, useLocation, useSearchParams } from "react-router";
import { AGENT_SESSION_CHAT_PARAM } from "./agentSessionLink";
import { ChatDetailSheet } from "./ChatDetailPanel";

/** Deletes a chat and refreshes every chat list; `onDeleted` runs after. */
function useDeleteChat(
  onDeleted: (chatId: string) => void,
): (chatId: string) => void {
  const queryClient = useQueryClient();
  const deleteChat = useChatDeleteMutation();
  return (chatId) => {
    deleteChat.mutate(
      { request: { id: chatId } },
      {
        onSuccess: () => {
          void invalidateAllListChats(queryClient);
          onDeleted(chatId);
        },
      },
    );
  };
}

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
  const [selectedChatId, setSelectedChatId] = useState<string | null>(null);
  const onDelete = useDeleteChat((chatId) =>
    setSelectedChatId((prev) => (prev === chatId ? null : prev)),
  );

  const sheet = (
    <ChatDetailSheet
      chatId={selectedChatId}
      onClose={() => setSelectedChatId(null)}
      onOpenChat={setSelectedChatId}
      onDelete={onDelete}
    />
  );

  return { selectedChatId, openChat: setSelectedChatId, sheet };
}

/**
 * Like `useChatDetailSheet`, but the open chat lives in the page's URL
 * (`?chatId=`, the same param Agent Sessions reads), so the drawer opens over
 * whatever page renders it, can be linked to, and opens and closes with the
 * browser's back and forward. Rows link to `chatHref(id)`, which keeps the
 * page's path and its other params.
 */
export function useChatDetailSheetInUrl(): {
  selectedChatId: string | null;
  chatHref: (chatId: string) => To;
  sheet: JSX.Element;
} {
  const location = useLocation();
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedChatId = searchParams.get(AGENT_SESSION_CHAT_PARAM);

  const chatHref = (chatId: string): To => {
    const next = new URLSearchParams(location.search);
    next.set(AGENT_SESSION_CHAT_PARAM, chatId);
    return { pathname: location.pathname, search: `?${next}` };
  };
  const openChat = (chatId: string) =>
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.set(AGENT_SESSION_CHAT_PARAM, chatId);
      return next;
    });
  // Closing is a history step of its own, like opening, so back reopens the
  // session the reader just closed. A deleted session cannot be reopened, so
  // closing it replaces the entry instead.
  const closeChat = (deletedChatId?: string) =>
    setSearchParams(
      (prev) => {
        if (
          deletedChatId &&
          prev.get(AGENT_SESSION_CHAT_PARAM) !== deletedChatId
        ) {
          return prev;
        }
        const next = new URLSearchParams(prev);
        next.delete(AGENT_SESSION_CHAT_PARAM);
        return next;
      },
      { replace: Boolean(deletedChatId) },
    );
  const onDelete = useDeleteChat(closeChat);

  const sheet = (
    <ChatDetailSheet
      chatId={selectedChatId}
      onClose={() => closeChat()}
      onOpenChat={openChat}
      onDelete={onDelete}
    />
  );

  return { selectedChatId, chatHref, sheet };
}
