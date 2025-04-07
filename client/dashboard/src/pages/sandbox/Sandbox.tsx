import { Page } from "@/components/page-layout";
import { useChat } from "@ai-sdk/react";
import { useCallback } from "react";
import AIChatWindow from "@/components/ai-chat/AIChatWindow";
import { AIChatSuggestion } from "@/components/ai-chat/types";
import { useProject } from "@/contexts/Auth";
import { smoothStream, streamText } from "ai";
import { useGramContext } from "@gram/sdk/react-query";
import { createOpenAI } from "@ai-sdk/openai";

export default function Sandbox() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <ChatWindow />
      </Page.Body>
    </Page>
  );
}

export function ChatWindow() {
  const project = useProject();
  const client = useGramContext();

  const openai = createOpenAI({
    apiKey: "this is required",
    baseURL: "http://localhost:8080/",
  });

  const fetch: typeof globalThis.fetch = async (input, init) => {
    console.log("fetch", input, JSON.parse(init?.body as string));

    const result = streamText({
      model: openai("gpt-4o"),
      messages: JSON.parse(init?.body as string).messages,
      experimental_transform: smoothStream({
        delayInMs: 20, // Looks a little smoother
      }),
    });

    return result.toDataStreamResponse();
  };

  const {
    messages,
    input,
    handleInputChange,
    handleSubmit,
    status,
    setMessages,
    append,
    addToolResult,
  } = useChat({
    // api: "/chat/completions",
    fetch,
    initialMessages: [
      {
        id: "initial",
        role: "user",
        content: "Hello, how are you?",
      },
    ],
    onFinish: (message) => {
      console.log("Chat finished with message:", message);
    },
    onError: (error) => {
      console.error("Chat error:", error);
    },
    maxSteps: 5,
    onToolCall: async ({ toolCall }) => {
      console.log("Received tool call on client:", toolCall);
      return undefined;
    },
  });

  const handleSend = useCallback(
    async (msg: string) => {
      await append({
        role: "user",
        content: msg,
      });

      setMessages((prev) => [
        ...prev,
        {
          role: "assistant",
          content: "",
          id: "streaming",
          parts: [
            {
              type: "text",
              text: "Thinking...",
            },
          ],
          data: {
            isStreaming: true,
          },
        },
      ]);
    },
    [append, setMessages]
  );

  const handleSuggestionClick = useCallback(
    (suggestion: AIChatSuggestion) => {
      void handleSend(suggestion.text);
    },
    [handleSend]
  );

  return (
    <div className="max-w-4xl rounded-2xl border h-full overflow-hidden">
      <AIChatWindow>
        <AIChatWindow.Conversation
          messages={messages}
          isGenerating={status === "streaming"}
          addToolResult={addToolResult as any}
        />
        <AIChatWindow.Prompt
          onSend={handleSend}
          disabled={status === "streaming"}
          onSuggestionClick={handleSuggestionClick}
        />
      </AIChatWindow>
    </div>
  );
}
