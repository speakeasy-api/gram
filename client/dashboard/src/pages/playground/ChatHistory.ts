import { useLoadChat } from "@gram/client/react-query";
import { UIMessage } from "ai";

export const useChatHistory = (chatId: string) => {
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

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const parts: Array<any> = [];

    // Handle text content
    if (message.content) {
      parts.push({
        type: "text",
        text: message.content,
      });
    }

    // Handle tool calls
    if (message.toolCalls) {
      const toolCalls = JSON.parse(message.toolCalls);
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      toolCalls.forEach((toolCall: any) => {
        const toolInvocation = getToolInvocation(toolCall.id);

        // Add tool call part
        parts.push({
          type: "tool-call",
          toolCallId: toolCall.id,
          tool: toolCall.function.name,
          input:
            typeof toolCall.function.arguments === "string"
              ? JSON.parse(toolCall.function.arguments)
              : toolCall.function.arguments,
          state: "output-available",
        });

        // Add tool result part if available
        if (toolInvocation?.content) {
          parts.push({
            type: "tool-result",
            toolCallId: toolCall.id,
            tool: toolCall.function.name,
            output: toolInvocation.content,
            state: "output-available",
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
