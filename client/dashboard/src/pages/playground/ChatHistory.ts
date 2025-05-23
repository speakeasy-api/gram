import { useLoadChat } from "@gram/client/react-query";
import { Message, ToolInvocation } from "ai";

export const useChatHistory = (chatId: string) => {
  const { data: loadedChat, isLoading } = useLoadChat(
    {
      id: chatId,
    },
    undefined,
    { retry: false } // Expected to fail (404) if it's a new chat
  );

  type ToolInvocationPart = {
    type: "tool-invocation";
    toolInvocation: ToolInvocation;
  };

  const chatHistory: Message[] = [];
  const messages = loadedChat?.messages ?? [];

  for (let i = 0; i < messages.length; i++) {
    const message = messages[i];
    if (!message) continue;
    if (message.role === "system") continue;

    const base = {
      id: message.id,
      role: message.role as Message["role"],
      content: message.content ?? "",
    };

    if (message.toolCalls) {
      // The next message is the tool call result
      const nextMessage = messages[i + 1];
      chatHistory.push({
        ...base,
        parts: JSON.parse(message.toolCalls).map(
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          (toolCall: any): ToolInvocationPart => ({
            type: "tool-invocation",
            toolInvocation: {
              state: "result",
              args: toolCall.function.arguments,
              toolCallId: toolCall.id,
              toolName: toolCall.function.name,
              result: nextMessage?.content ?? "",
            },
          })
        ),
      });
      // Skip the next message since we used it as the tool result
      i++;
    } else {
      chatHistory.push(base);
    }
  }

  return { chatHistory, isLoading };
};
