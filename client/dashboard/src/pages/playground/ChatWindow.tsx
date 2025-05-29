import { AutoSummarizeBadge } from "@/components/auto-summarize-badge";
import { HttpRoute } from "@/components/http-route";
import { ProjectAvatar } from "@/components/project-menu";
import { Heading } from "@/components/ui/heading";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useProject, useSession } from "@/contexts/Auth";
import { cn, getServerURL } from "@/lib/utils";
import { Message, useChat } from "@ai-sdk/react";
import {
  useInstance,
  useToolsetSuspense,
} from "@gram/client/react-query/index.js";
import { createOpenRouter } from "@openrouter/ai-sdk-provider";
import {
  AIChatContainer,
  Stack,
  useToolCallApproval,
} from "@speakeasy-api/moonshine";
import {
  generateObject,
  jsonSchema,
  smoothStream,
  streamText,
  Tool,
  ToolCall,
} from "ai";
import Ajv from "ajv";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { v7 as uuidv7 } from "uuid";
import { z } from "zod";
import { McpToolsetCard } from "../mcp/MCP";
import { SdkContent } from "../sdk/SDK";
import { AgentifyButton } from "./Agentify";
import { useChatHistory } from "./ChatHistory";
import { PanelHeader, useChatContext } from "./Playground";

// Ignore int32 format instead of erroring out
const ajv = new Ajv({ formats: { int32: true } });

const availableModels = [
  { label: "GPT-4o", value: "openai/gpt-4o" },
  { label: "GPT-4o-mini", value: "openai/gpt-4o-mini" },
  { label: "GPT-4.1", value: "openai/gpt-4.1" },
  { label: "GPT-4.1 Mini", value: "openai/gpt-4.1-mini" },
  { label: "o3 Mini", value: "openai/o3-mini" },
  { label: "Claude 3.7 Sonnet", value: "anthropic/claude-3.7-sonnet" },
  { label: "Claude 3.5 Sonnet", value: "anthropic/claude-3.5-sonnet" },
  { label: "Claude 3.5 Haiku", value: "anthropic/claude-3.5-haiku" },
  { label: "Gemini 2.5 Pro Preview", value: "google/gemini-2.5-pro-preview" },
  { label: "Mistral Medium 3", value: "mistralai/mistral-medium-3" },
  { label: "Mistral Codestral 2501", value: "mistralai/codestral-2501" },
];

export type ChatConfig = React.RefObject<{
  toolsetSlug: string | null;
  environmentSlug: string | null;
  isOnboarding: boolean;
}>;

export function ChatWindow({
  configRef,
  chatId,
  dynamicToolset,
}: {
  configRef: ChatConfig;
  chatId: string;
  dynamicToolset: boolean;
}) {
  const [model, setModel] = useState(availableModels[0]?.value ?? "");
  const chatKey = `chat-${model}`;

  // We do this because we want the chat to reset when the model changes
  return (
    <ChatInner
      key={chatKey}
      model={model}
      setModel={setModel}
      configRef={configRef}
      chatId={chatId}
      dynamicToolset={dynamicToolset}
    />
  );
}

type Toolset = Record<
  string,
  Tool & { id: string; method?: string; path?: string }
>;

function ChatInner({
  model,
  setModel,
  configRef,
  chatId,
  dynamicToolset,
}: {
  model: string;
  setModel: (model: string) => void;
  configRef: ChatConfig;
  chatId: string;
  dynamicToolset: boolean;
}) {
  const session = useSession();
  const project = useProject();
  const { setMessages } = useChatContext();
  const { chatHistory, isLoading: isChatHistoryLoading } =
    useChatHistory(chatId);
  const [displayOnlyMessages, setDisplayOnlyMessages] = useState<Message[]>([]);

  const selectedTools = useRef<string[]>([]);

  const instance = useInstance(
    {
      toolsetSlug: configRef.current.toolsetSlug ?? "",
      environmentSlug: configRef.current.environmentSlug ?? undefined,
    },
    undefined,
    {
      enabled:
        !!configRef.current.toolsetSlug && !!configRef.current.environmentSlug,
    }
  );

  const appendDisplayOnlyMessage = useCallback(
    (message: string) =>
      setDisplayOnlyMessages((prev) => [
        ...prev,
        {
          id: uuidv7(),
          role: "system",
          content: `unused`,
          parts: [
            {
              type: "text",
              text: message,
            },
          ],
          createdAt: new Date(),
        },
      ]),
    []
  );

  const allTools: Toolset = useMemo(
    () =>
      Object.fromEntries(
        instance.data?.tools.map((tool) => {
          return [
            tool.name,
            {
              id: tool.id,
              method: tool.httpMethod,
              path: tool.path,
              parameters: jsonSchema(
                tool.schema ? JSON.parse(tool.schema) : {}
              ),
            },
          ];
        }) ?? []
      ),
    [instance.data?.tools]
  );

  const openrouterChat = createOpenRouter({
    apiKey: "this is required",
    baseURL: getServerURL(),
    headers: {
      "Gram-Session": session.session,
      "Gram-Project": project.slug,
      "Gram-Chat-ID": chatId,
    },
  });

  const openrouterBasic = createOpenRouter({
    apiKey: "this is required",
    baseURL: getServerURL(),
    headers: {
      "Gram-Session": session.session,
      "Gram-Project": project.slug,
    },
  });

  const updateToolsTool = {
    id: "refresh_tools",
    name: "refresh_tools",
    description: `If you are unable to fulfill the user's request with the current set of tools, use this tool to get a new set of tools.
    The request is a description of the task you are trying to complete based on the conversation history. 
    Try to incorporate not just the most recent messages, but also the overall task the user has been trying to accomplish over the course of the chat.`,
    parameters: z.object({
      priorConversationSummary: z.string(),
      previouslyUsedTools: z.array(z.string()),
      newRequest: z.string(),
    }),
  };

  const updateSelectedTools = async (task: string) => {
    if (!dynamicToolset || Object.keys(allTools).length === 0) return;

    const toolsString = Object.entries(allTools)
      .map(([tool, toolInfo]) => `${tool}: ${toolInfo.description}`)
      .join("\n");

    const result = await generateObject({
      model: openrouterBasic.chat(model),
      prompt: `Below is a list of tools and a description of the task I want to complete. Please return a list of tool names that you think are relevant to the task.
      Include any tools that you think might be useful for answering follow-up questions, taking into account the conversation history.
      Try to return between 5 and 25 tools.
      Try to include tools that were used in the past if they fit.
      Tools: ${toolsString}
      Task: ${task}`,
      temperature: 0.5,
      schema: z.object({
        tools: z.array(z.string()),
      }),
    });

    selectedTools.current = [...result.object.tools, updateToolsTool.name];

    appendDisplayOnlyMessage(
      `**Updated tool list:** *${result.object.tools.join(", ")}*`
    );
  };

  const updateSelectedToolsFromMessages = async (messages: Message[]) => {
    const task = messages.map((m) => `${m.role}: ${m.content}`).join("\n");
    await updateSelectedTools(task);
  };

  const openaiFetch: typeof globalThis.fetch = async (_, init) => {
    const messages = JSON.parse(init?.body as string).messages;

    let tools = allTools;

    // On the first message, get the initial set of tools if we're using a dynamic toolset
    if (dynamicToolset && selectedTools.current.length === 0) {
      await updateSelectedToolsFromMessages(messages);
    }

    if (dynamicToolset) {
      tools = Object.fromEntries(
        Object.entries(allTools).filter(([tool]) =>
          selectedTools.current.includes(tool)
        )
      );
    }

    let systemPrompt = `You are a helpful assistant that can answer questions and help with tasks.
        When using tools, ensure that the arguments match the provided schema. Note that the schema may update as the conversation progresses.
        The current date is ${new Date().toISOString()}`;

    if (dynamicToolset) {
      systemPrompt += `
        If you are unable to fulfill the user's request with the current set of tools, use the refresh_tools tool to get a new set of tools before saying you can't do it. 
        The current date is ${new Date().toISOString()}`;
    }

    const result = streamText({
      model: openrouterChat.chat(model),
      messages,
      tools,
      temperature: 0.5,
      system: systemPrompt,
      experimental_transform: smoothStream({
        delayInMs: 15, // Looks a little smoother
      }),
      onError: (event: { error: unknown }) => {
        let displayMessage: string | undefined;
        if (typeof event.error === 'object' && event.error !== null) {
          const errorObject = event.error as { responseBody?: unknown; message?: unknown; [key: string]: unknown };

          if (typeof errorObject.responseBody === "string") {
            try {
              const parsedBody = JSON.parse(errorObject.responseBody);
              if (typeof parsedBody === 'object' && parsedBody !== null && parsedBody.error && typeof parsedBody.error.message === "string") {
                displayMessage = parsedBody.error.message;
              }
            } catch(e) {
              console.error(`Error parsing model error: ${e}`);
            }
          } else if (typeof errorObject.message === 'string') {
            displayMessage = errorObject.message;
          }
        }
        if (displayMessage) {
          // some manipulation to promote summarization
          if (displayMessage.includes("maximum context length")) {
            const cutoffPhrase = "Please reduce the length of either one";
            const cutoffIndex = displayMessage.indexOf(cutoffPhrase);
            if (cutoffIndex !== -1) {
              displayMessage = displayMessage.substring(0, cutoffIndex);
            }
            displayMessage += " Please consider enabling *Auto-Summarize* for your tool or revise your prompt.";
          }
          appendDisplayOnlyMessage(`**Model Error:** *${displayMessage}*`);
        }
      },
    });

    return result.toDataStreamResponse();
  };

  const initialMessages: Message[] = configRef.current.isOnboarding
    ? [
        {
          id: "1",
          role: "assistant",
          content:
            "Welcome to Gram! Upload an OpenAPI document to get started.",
        },
      ]
    : chatHistory ?? [];

  const toolCallApproval = useToolCallApproval({
    executeToolCall: async (toolCall) => {
      if (toolCall.toolName === updateToolsTool.name) {
        const args = updateToolsTool.parameters.parse(toolCall.args);
        await updateSelectedTools(JSON.stringify(args));
        return `Updated tool list: ${selectedTools.current.join(", ")}`;
      }

      const tool = allTools[toolCall.toolName];
      if (!tool) {
        throw new Error(`Tool ${toolCall.toolName} not found`);
      }

      const response = await fetch(
        `${getServerURL()}/rpc/instances.invoke/tool?tool_id=${
          tool.id
        }&environment_slug=${configRef.current.environmentSlug}`,
        {
          method: "POST",
          headers: {
            "gram-session": session.session,
            "gram-project": project.slug,
          },
          body: JSON.stringify(toolCall.args),
        }
      );

      const result = await response.text();

      return result || `status code: ${response.status}`;
    },
    requiresApproval: (toolCall) => {
      const tool = allTools[toolCall.toolName];
      if (tool?.method === "GET") {
        return false;
      }
      return toolCall.toolName !== updateToolsTool.name;
    },
  });

  const validateArgs = (toolCall: ToolCall<string, unknown>) => {
    const tool = allTools[toolCall.toolName];
    if (!tool) {
      throw new Error(`Tool ${toolCall.toolName} not found`);
    }

    const validate = ajv.compile(tool.parameters.jsonSchema);
    if (!validate(toolCall.args)) {
      return "Schema validation error: " + JSON.stringify(validate.errors);
    }
    return null;
  };

  const {
    messages: chatMessages,
    status,
    append,
  } = useChat({
    id: chatId,
    fetch: openaiFetch,
    onError: (error) => {
      console.error("Chat error:", error.message, error.stack);
      // don't write display message for non useful obscured onChat error. StreamText will handle it if it's a model error.
      if (error.message.trim() !== "An error occurred.") {
        appendDisplayOnlyMessage(`**Error:** *${error.message}*`);
      }
    },
    maxSteps: 5,
    initialMessages,
    onToolCall: (toolCall) => {
      try {
        const validationError = validateArgs(toolCall.toolCall);
        if (validationError) {
          appendDisplayOnlyMessage(`**Warning:** *${validationError}*`);
        }
      } catch (e) {
        appendDisplayOnlyMessage(`**Warning:** *${e}*`);
      }

      return toolCallApproval.toolCallFn(toolCall);
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

  useEffect(() => {
    setMessages(chatMessages);
  }, [chatMessages]);

  const messagesToDisplay = [...displayOnlyMessages, ...chatMessages];
  messagesToDisplay.sort((a, b) => {
    if (a.createdAt && b.createdAt) {
      if (a.createdAt.getTime() === b.createdAt.getTime()) {
        return a.role === "system" ? -1 : 1;
      }
      return a.createdAt.getTime() - b.createdAt.getTime();
    }
    return 0;
  });

  const agentifyButton = (
    <AgentifyButton
      toolsetSlug={configRef.current.toolsetSlug ?? ""}
      environmentSlug={configRef.current.environmentSlug ?? ""}
      key="agentify-button"
      onAgentify={() => setActiveTab("agents")}
    />
  );

  const [activeTab, setActiveTab] = useState<"chat" | "agents" | "mcp">("chat");

  // TODO: fix this
  /* eslint-disable  @typescript-eslint/no-explicit-any */
  const m = messagesToDisplay as any;

  return (
    <Tabs
      value={activeTab}
      onValueChange={(value) =>
        setActiveTab(value as "chat" | "agents" | "mcp")
      }
      className="h-full relative"
    >
      <PanelHeader side="right">
        <Stack direction="horizontal" gap={2} align="center">
          <Type className="font-medium">Use with: </Type>
          <TabsList className="bg-stone-200 dark:bg-stone-800">
            <TabsTrigger value="chat">Chat</TabsTrigger>
            <TabsTrigger value="agents">Agents</TabsTrigger>
            <TabsTrigger value="mcp">MCP</TabsTrigger>
          </TabsList>
        </Stack>
      </PanelHeader>
      <div className="h-[calc(100%-61px)] pl-8 pr-4 pt-4">
        <TabsContent value="chat" className="h-full">
          <AIChatContainer
            messages={m}
            isLoading={status === "streaming" || isChatHistoryLoading}
            onSendMessage={handleSend}
            className={"pb-4"}
            toolCallApproval={toolCallApproval}
            components={{
              composer: {
                additionalActions: [agentifyButton],
              },
              message: {
                avatar: {
                  user: () => (
                    <ProjectAvatar project={project} className="h-6 w-6" />
                  ),
                },
                toolCall: toolCallComponents(allTools),
              },
            }}
            modelSelector={{
              model,
              onModelChange: setModel,
              availableModels,
            }}
          />
        </TabsContent>
        <TabsContent
          value="agents"
          className="h-full overflow-scroll pb-4 pr-4"
        >
          <SdkContent
            toolset={configRef.current.toolsetSlug ?? undefined}
            environment={configRef.current.environmentSlug ?? undefined}
          />
        </TabsContent>
        <TabsContent value="mcp">
          {configRef.current.toolsetSlug ? (
            <McpTab toolsetSlug={configRef.current.toolsetSlug} />
          ) : (
            <div className="text-muted-foreground">No toolset selected</div>
          )}
        </TabsContent>
      </div>
    </Tabs>
  );
}

const McpTab = ({ toolsetSlug }: { toolsetSlug: string }) => {
  const { data: toolset, refetch } = useToolsetSuspense({ slug: toolsetSlug });

  return (
    <Stack gap={8}>
      <div>
        <Heading variant="h2">Hosted MCP Servers</Heading>
        <Type className="text-muted-foreground mt-2">
          Expose this toolset as a hosted MCP server
        </Type>
      </div>
      <McpToolsetCard toolset={toolset} onUpdate={refetch} />
    </Stack>
  );
};

const toolCallComponents = (tools: Toolset) => {
  const JsonDisplay = ({ json }: { json: string }) => {
    let pretty = json;
    // Particularly when loading chat history from the database, the JSON formatting needs to be restored
    if (json.startsWith('"') && json.endsWith('"')) {
      pretty = json.slice(1, -1);
      pretty = pretty.replace(/\\"/g, '"');
      pretty = pretty.replace(/\\n/g, "\n");
    }
    try {
      pretty = JSON.stringify(JSON.parse(pretty), null, 2);
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
    } catch (e) {
      // If its not JSON, that's ok, we'll just return the string
    }

    return (
      <pre className="typography-body-xs max-h-48 overflow-auto rounded bg-neutral-900 p-2 break-all whitespace-pre-wrap text-neutral-300">
        {pretty}
      </pre>
    );
  };

  return {
    toolName: ({
      toolName,
      result,
      args,
    }: {
      toolName: string;
      result?: unknown;
      args: Record<string, unknown>;
    }) => {
      const hasSummary = JSON.stringify(args).includes("gram-request-summary");
      const validationError =
        typeof result === "string" &&
        result.includes("Schema validation error");

      return (
        <Stack
          direction="horizontal"
          gap={2}
          align="center"
          className="mr-auto"
        >
          <Type
            variant="small"
            className={cn(
              "font-medium",
              validationError && "line-through text-muted-foreground"
            )}
          >
            {toolName}
          </Type>
          {validationError && (
            <Type variant="small" muted>
              (invalid args)
            </Type>
          )}
          {hasSummary && <AutoSummarizeBadge />}
        </Stack>
      );
    },
    input: (props: { toolName: string; args: string }) => {
      const tool = tools[props.toolName];
      return (
        <Stack gap={2}>
          {tool?.method && tool?.path && (
            <HttpRoute method={tool.method} path={tool.path} />
          )}
          <JsonDisplay json={props.args} />
        </Stack>
      );
    },
    result: (props: {
      toolName: string;
      result: string;
      args: Record<string, unknown>;
    }) => {
      const tool = tools[props.toolName];
      const hasSummary = JSON.stringify(props.args).includes(
        "gram-request-summary"
      );

      return (
        <Stack gap={2} className="mt-4">
          <Type variant="small" className="font-medium text-muted-foreground">
            {tool?.method ? "Response Body" : "Result"}
            {hasSummary && (
              <span className="text-muted-foreground/50">
                {" "}
                (auto-summarized)
              </span>
            )}
          </Type>
          <JsonDisplay json={props.result} />
        </Stack>
      );
    },
  };
};
