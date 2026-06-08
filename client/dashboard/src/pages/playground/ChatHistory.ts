import { useLoadChat } from "@gram/client/react-query";
import { UIMessage } from "ai";

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

export const useChatHistory = (
  chatId: string,
): { chatHistory: UIMessage[]; isLoading: boolean } => {
  const { data: loadedChat, isLoading } = useLoadChat(
    {
      id: chatId,
    },
    undefined,
    { retry: false, throwOnError: false }, // Expected to fail (404) if it's a new chat
  );

  const chatHistory: UIMessage[] = [];
  const messages = loadedChat?.messages ?? [];

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

  chatHistory.sort((_a, _b) => {
    // Since we don't have createdAt in UIMessage, keep original order
    return 0;
  });

  return { chatHistory, isLoading };
};
