import { Page } from "@/components/page-layout";
import { useChat } from "@ai-sdk/react";
import { useCallback } from "react";
import { AIChatContainer } from "@speakeasy-api/moonshine";
import { useProject, useSession } from "@/contexts/Auth";
import { smoothStream, streamText } from "ai";
import { createOpenAI } from "@ai-sdk/openai";
import {
  useLoadInstanceSuspense,
  useToolsetSuspense,
} from "@gram/sdk/react-query";
import { jsonSchema } from "ai";

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
  const session = useSession();
  const project = useProject();

  const instance = useLoadInstanceSuspense(
    {},
    {
      gramProject: project.projectSlug,
      toolsetSlug: "my-test",
      environmentSlug: "test3",
    }
  );

  console.log(instance);

  const tools = Object.fromEntries(
    instance.data.tools.map((tool) => [
      tool.name,
      {
        id: tool.id,
        description: tool.description,
        parameters: jsonSchema(JSON.parse(tool.schema)),
      },
    ])
  );

  console.log(tools);

  const openai = createOpenAI({
    apiKey: "this is required",
    baseURL: "http://localhost:8080/",
    headers: {
      "Gram-Session": session.session,
    },
  });

  const openaiFetch: typeof globalThis.fetch = async (_, init) => {
    const result = streamText({
      model: openai("gpt-4o"),
      messages: JSON.parse(init?.body as string).messages,
      tools,
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
    fetch: openaiFetch,
    onFinish: (message) => {
      console.log("Chat finished with message:", message);
    },
    onError: (error) => {
      console.error("Chat error:", error.message, error.stack);
    },
    maxSteps: 5,
    onToolCall: async ({ toolCall }) => {
      const tool = tools[toolCall.toolName];

      console.log("Received new tool call:", toolCall);

      const response = await fetch(
        `http://localhost:8080/rpc/instances.invoke/tool?tool_id=${tool.id}&environment_slug=test3`,
        {
          method: "POST",
          headers: {
            "gram-session": session.session,
            "gram-project": project.projectSlug,
          },
          body: JSON.stringify(toolCall.args),
        }
      )

      const result = await response.json();

      console.log("tool result", result);

      return result;
    },
  });

  const handleSend = useCallback(
    async (msg: string) => {
      await append({
        role: "user",
        content: msg,
      });
    },
    [append]
  );

  return (
    <AIChatContainer
      messages={messages}
      isLoading={status === "streaming"}
      onSendMessage={handleSend}
      className="max-w-4xl"
    />
  );
}
