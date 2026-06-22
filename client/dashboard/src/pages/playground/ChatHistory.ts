import { useEffect, useMemo } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { UIMessage } from "ai";
import { useSdkClient } from "@/contexts/Sdk";

// Stored tool call shape (OpenAI-style payload persisted as a JSON blob in
// ChatMessage.toolCalls). We don't import this from the SDK because the SDK
// only models the surrounding ChatMessage and leaves the toolCalls payload as
// an opaque JSON string.
type StoredToolCall = {
  id: string;
  function: {
    name: string;
    arguments: string | Record<string, unknown>;
  };
};

// chat.load now paginates by seq keyset. The playground replays the *entire*
// saved conversation into the chat state (and re-sends it to the model on the
// next turn), so we page to exhaustion here rather than rendering a single
// window. A dedicated query key keeps this from colliding with the chat detail
// sheet's paginated transcript cache for the same chat.
const HISTORY_PAGE_SIZE = 200;

export const useChatHistory = (
  chatId: string,
): { chatHistory: UIMessage[]; isLoading: boolean } => {
  const client = useSdkClient();

  const query = useInfiniteQuery({
    queryKey: ["chat", chatId, "history-replay"],
    initialPageParam: undefined as number | undefined,
    queryFn: ({ pageParam }) =>
      client.chat.load({
        id: chatId,
        limit: HISTORY_PAGE_SIZE,
        ...(pageParam !== undefined ? { beforeSeq: pageParam } : {}),
      }),
    // "next" page = older messages; walk back until the start of the chat.
    getNextPageParam: (lastPage) =>
      lastPage.hasMoreBefore ? lastPage.messages[0]?.seq : undefined,
    retry: false, // Expected to 404 if it's a new chat.
    throwOnError: false,
  });

  // Eagerly pull every older page so the replayed history is complete.
  useEffect(() => {
    if (query.hasNextPage && !query.isFetchingNextPage) {
      void query.fetchNextPage();
    }
  }, [query.hasNextPage, query.isFetchingNextPage, query]);

  // Pages arrive newest-first; reverse them so the flattened list is ascending.
  const messages = useMemo(
    () => [...(query.data?.pages ?? [])].reverse().flatMap((p) => p.messages),
    [query.data],
  );

  const chatHistory: UIMessage[] = [];

  const toolInvocations = messages.filter((m) => m.role === "tool");
  const getToolInvocation = (id: string) => {
    return toolInvocations.find((t) => t.toolCallId === id);
  };

  for (const message of messages) {
    if (!message) continue;
    if (message.role === "system" || message.role === "tool") continue;

    const parts: UIMessage["parts"] = [];

    // Handle text content
    if (message.content) {
      parts.push({
        type: "text",
        text: message.content,
      });
    }

    // Handle tool calls
    if (message.toolCalls) {
      const toolCalls = JSON.parse(message.toolCalls) as StoredToolCall[];
      toolCalls.forEach((toolCall: StoredToolCall) => {
        const toolInvocation = getToolInvocation(toolCall.id);
        const input =
          typeof toolCall.function.arguments === "string"
            ? (JSON.parse(toolCall.function.arguments) as unknown)
            : toolCall.function.arguments;

        // Replay the tool invocation as a single ToolUIPart with output if
        // the result is known, or input-available otherwise.
        if (toolInvocation?.content) {
          parts.push({
            type: `tool-${toolCall.function.name}`,
            toolCallId: toolCall.id,
            state: "output-available",
            input,
            output: toolInvocation.content,
          });
        } else {
          parts.push({
            type: `tool-${toolCall.function.name}`,
            toolCallId: toolCall.id,
            state: "input-available",
            input,
          });
        }
      });
    }

    chatHistory.push({
      id: message.id,
      role: message.role as UIMessage["role"],
      parts,
    });
  }

  // isLoading stays true until every older page is in, so the playground never
  // replays a partial transcript and then jumps.
  return {
    chatHistory,
    isLoading: query.isLoading || query.hasNextPage,
  };
};
