import { useLoadChat } from "@gram/client/react-query";
import { Message, ToolInvocation } from "ai";

export const useChatHistory = (chatId: string) => {
  const { data: loadedChat, isLoading } = useLoadChat(
    {
      id: chatId,
    },
    undefined,
    { retry: false, throwOnError: false }, // Expected to fail (404) if it's a new chat
  );

  type ToolInvocationPart = {
    type: "tool-invocation";
    toolInvocation: ToolInvocation;
  };

  const chatHistory: Message[] = [];
  const messages = loadedChat?.messages ?? [];

  const toolInvocations = messages.filter((m) => m.role === "tool");
  const getToolInvocation = (id: string) => {
    return toolInvocations.find((t) => t.toolCallId === id);
  };

  for (const message of messages) {
    if (!message) continue;
    if (message.role === "system" || message.role === "tool") continue;

    const base = {
      id: message.id,
      role: message.role as Message["role"],
      content: message.content ?? "",
      createdAt: message.createdAt,
    };

    if (message.toolCalls) {
      // Find the tool invocation for the tool call and add it to the parts
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
              result: getToolInvocation(toolCall.id)?.content ?? "",
            },
          }),
        ),
      });
    } else {
      chatHistory.push(base);
    }
  }

  chatHistory.sort((a, b) => {
    const aTime = a.createdAt?.getTime() ?? 0;
    const bTime = b.createdAt?.getTime() ?? 0;
    return bTime - aTime;
  });

  return { chatHistory, isLoading };
};
