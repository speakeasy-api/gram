import { AssistantRuntimeProvider } from "@assistant-ui/react";
import React from "react";
import {
  useChatRuntime,
  AssistantChatTransport,
} from "@assistant-ui/react-ai-sdk";

/**
 * Global decorator that wraps all stories in the AssistantRuntimeProvider,
 * which provides the chat runtime to the story.
 * Note: This assumes that all stories require a chat runtime, but we move back to
 * per story decorator in the future.
 * @param children - The children to render.
 * @returns
 */
export const GlobalDecorator: React.FC<{ children: React.ReactNode }> = ({
  children,
}: {
  children: React.ReactNode;
}) => {
  const runtime = useChatRuntime({
    transport: new AssistantChatTransport({
      api: "/api/chat",
    }),
  });
  return (
    <AssistantRuntimeProvider runtime={runtime}>
      {children}
    </AssistantRuntimeProvider>
  );
};
