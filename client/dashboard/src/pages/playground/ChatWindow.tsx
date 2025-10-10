import { ChatInput } from "@/components/chat/ChatInput";
import { ChatMessages } from "@/components/chat/ChatMessages";
import { HttpRoute } from "@/components/http-route";
import { ProjectAvatar } from "@/components/project-menu";
import { Slider } from "@/components/ui/slider";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useProject, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { asTool, filterHttpTools, filterPromptTools } from "@/lib/toolTypes";
import { cn, getServerURL } from "@/lib/utils";
import { useChat } from "@ai-sdk/react";
import { HTTPToolDefinition } from "@gram/client/models/components";
import { useInstance } from "@gram/client/react-query/index.js";
import { Stack } from "@speakeasy-api/moonshine";
import {
  generateObject,
  jsonSchema,
  UIMessage,
} from "ai";
import { CustomChatTransport } from "@/lib/CustomChatTransport";

type CoreTool = {
  description?: string;
  inputSchema: unknown;
  execute?: (input: unknown) => unknown | Promise<unknown>;
};
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { v7 as uuidv7 } from "uuid";
import { z } from "zod";
import { onboardingStepStorageKeys } from "../home/Home";
import { useChatContext } from "./ChatContext";
import { useChatHistory } from "./ChatHistory";
import { MessageHistoryIndicator } from "./MessageHistoryIndicator";
import { useMiniModel, useModel } from "./Openrouter";
import { useMessageHistoryNavigation } from "./useMessageHistoryNavigation";
import { parseMentionedTools, Tool as MentionTool } from "./ToolMentions";
import { ChatComposerWrapper } from "./ChatComposerWrapper";
import { Combobox } from "@/components/ui/combobox";

const defaultModel = {
  label: "Claude 4.5 Sonnet",
  value: "anthropic/claude-sonnet-4.5",
};

const availableModels = [
  defaultModel,
  { label: "Claude 4 Sonnet", value: "anthropic/claude-sonnet-4" },
  { label: "GPT-4o", value: "openai/gpt-4o" },
  { label: "GPT-4o-mini", value: "openai/gpt-4o-mini" },
  { label: "GPT-5", value: "openai/gpt-5" },
  { label: "GPT-4.1", value: "openai/gpt-4.1" },
  { label: "Claude 3.7 Sonnet", value: "anthropic/claude-3.7-sonnet" },
  { label: "Claude 4 Opus (Expensive)", value: "anthropic/claude-opus-4" },
  { label: "Gemini 2.5 Pro Preview", value: "google/gemini-2.5-pro-preview" },
  { label: "Kimi K2", value: "moonshotai/kimi-k2" },
  { label: "Mistral Medium 3", value: "mistralai/mistral-medium-3" },
  { label: "Mistral Codestral 2501", value: "mistralai/codestral-2501" },
];

export type ChatConfig = React.RefObject<{
  toolsetSlug: string | null;
  environmentSlug: string | null;
}>;

const MAX_TOOL_RESPONSE_LENGTH = 50_000; // Characters

export function ChatWindow({
  configRef,
  dynamicToolset = false,
  additionalActions,
  initialMessages,
  initialPrompt,
}: {
  configRef: ChatConfig;
  dynamicToolset?: boolean;
  additionalActions?: React.ReactNode;
  initialMessages?: UIMessage[];
  initialPrompt?: string | null;
}) {
  const [model, setModel] = useState(defaultModel.value);
  const [temperature, setTemperature] = useState(0.5);
  const chatKey = `chat-${model}`;

  // We do this because we want the chat to reset when the model changes
  return (
    <ChatInner
      key={chatKey}
      model={model}
      setModel={setModel}
      temperature={temperature}
      setTemperature={setTemperature}
      configRef={configRef}
      dynamicToolset={dynamicToolset}
      initialMessages={initialMessages}
      additionalActions={additionalActions}
      initialPrompt={initialPrompt}
    />
  );
}

type Toolset = Record<
  string,
  CoreTool & { id?: string; method?: string; path?: string }
>;

function ChatInner({
  model,
  setModel,
  temperature,
  setTemperature,
  configRef,
  dynamicToolset,
  initialMessages,
  additionalActions,
  initialPrompt,
}: {
  model: string;
  setModel: (model: string) => void;
  temperature: number;
  setTemperature: (temperature: number) => void;
  configRef: ChatConfig;
  dynamicToolset: boolean;
  initialMessages?: UIMessage[];
  additionalActions?: React.ReactNode;
  initialPrompt?: string | null;
}) {
  const session = useSession();
  const project = useProject();
  const telemetry = useTelemetry();
  const client = useSdkClient();

  const chat = useChatContext();
  const { setMessages } = chat;
  const { chatHistory, isLoading: isChatHistoryLoading } = useChatHistory(
    chat.id,
  );

  const [displayOnlyMessages, setDisplayOnlyMessages] = useState<UIMessage[]>(
    [],
  );
  const selectedTools = useRef<string[]>([]);
  const [_mentionedToolIds, setMentionedToolIds] = useState<string[]>([]);
  const [_inputText, setInputText] = useState("");

  // Feature flag for experimental tool tagging syntax
  const isToolTaggingEnabled = telemetry.isFeatureEnabled(
    "gram-experimental-chat",
  );

  const instance = useInstance(
    {
      toolsetSlug: configRef.current.toolsetSlug ?? "",
      environmentSlug: configRef.current.environmentSlug ?? undefined,
    },
    undefined,
    {
      enabled:
        !!configRef.current.toolsetSlug && !!configRef.current.environmentSlug,
    },
  );

  const appendDisplayOnlyMessage = useCallback(
    (message: string) =>
      setDisplayOnlyMessages((prev) => [
        ...prev,
        {
          id: uuidv7(),
          role: "system",
          parts: [
            {
              type: "text",
              text: message,
            },
          ],
          createdAt: new Date(),
        },
      ]),
    [],
  );

  const executeHttpToolFn =
    (tool: HTTPToolDefinition, toolsetSlug: string) =>
    async (args: unknown) => {
      const response = await fetch(
        `${getServerURL()}/rpc/instances.invoke/tool?tool_id=${tool.id}&environment_slug=${
          configRef.current.environmentSlug
        }&chat_id=${chat.id}&toolset_slug=${toolsetSlug}`,
        {
          method: "POST",
          headers: {
            "gram-session": session.session,
            "gram-project": project.slug,
          },
          body: JSON.stringify(args),
        },
      );

      const result = await response.text();

      if (result.length > MAX_TOOL_RESPONSE_LENGTH) {
        return (
          "Response is too long and has been truncated to avoid bricking your playground's context window. Consider using [response filtering](https://docs.getgram.ai/concepts/openapi#response-filtering) to help the LLM process API responses more effectively." +
          `\n\n${result.slice(0, MAX_TOOL_RESPONSE_LENGTH)}`
        );
      }

      return result || `status code: ${response.status}`;
    };

  const allTools: Toolset = useMemo(() => {
    console.log("allTools: instance.data?.tools:", instance.data?.tools);
    console.log("allTools: toolsetSlug:", configRef.current.toolsetSlug);
    console.log("allTools: environmentSlug:", configRef.current.environmentSlug);
    const baseTools = instance.data?.tools.map(asTool);

    const tools: Toolset = Object.fromEntries(
      filterHttpTools(baseTools).map((tool) => {
        return [
          tool.name,
          {
            id: tool.id,
            method: tool.httpMethod,
            path: tool.path,
            description: tool.description,
            inputSchema: jsonSchema(tool.schema ? JSON.parse(tool.schema) : {}),
            execute: executeHttpToolFn(
              tool,
              configRef.current.toolsetSlug || "",
            ),
          },
        ];
      }) ?? [],
    );

    filterPromptTools(baseTools).forEach((pt) => {
      tools[pt.name] = {
        id: pt.id as string,
        description: pt.description ?? "",
        inputSchema: jsonSchema(JSON.parse(pt.schema ?? "{}")),
        execute: async (args: unknown) => {
          const res = await client.templates.renderByID({
            id: pt.id as `${string}.${string}`,
            renderTemplateByIDRequestBody: {
              arguments: args as Record<string, unknown>,
            },
          });

          return res.prompt;
        },
      };
    });

    return tools;
  }, [instance.data]);

  // Create a list of tools for the mention system
  const mentionTools: MentionTool[] = useMemo(() => {
    return Object.entries(allTools).map(([name, tool]) => {
      const toolWithId = tool as CoreTool & { id?: string };
      return {
        id: toolWithId.id || name,
        name,
        description: tool.description,
        type: tool.method ? "http" : "prompt",
        httpMethod: tool.method,
        path: tool.path,
      };
    });
  }, [allTools]);

  const openrouterChat = useModel(model, {
    "Gram-Chat-ID": chat.id,
  });

  const openrouterBasic = useMiniModel();

  const updateToolsTool: CoreTool & { name: string } = {
    name: "refresh_tools",
    description: `If you are unable to fulfill the user's request with the current set of tools, use this tool to get a new set of tools.
    The request is a description of the task you are trying to complete based on the conversation history.
    Try to incorporate not just the most recent messages, but also the overall task the user has been trying to accomplish over the course of the chat.`,
    inputSchema: z.object({
      priorConversationSummary: z.string(),
      previouslyUsedTools: z.array(z.string()),
      newRequest: z.string(),
    }),
    execute: async (args: unknown) => {
      const parsedArgs = z
        .object({
          priorConversationSummary: z.string(),
          previouslyUsedTools: z.array(z.string()),
          newRequest: z.string(),
        })
        .parse(args);
      await updateSelectedTools(JSON.stringify(parsedArgs));
      return `Updated tool list: ${selectedTools.current.join(", ")}`;
    },
  };

  const updateSelectedTools = async (task: string) => {
    if (!dynamicToolset || Object.keys(allTools).length === 0) return;

    const toolsString = Object.entries(allTools)
      .map(([tool, toolInfo]) => `${tool}: ${toolInfo.description}`)
      .join("\n");

    const result = await generateObject({
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      model: openrouterBasic as any,
      mode: "json",
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

    selectedTools.current = [
      ...(result.object as { tools: string[] }).tools,
      updateToolsTool.name,
    ];

    appendDisplayOnlyMessage(
      `**Updated tool list:** *${(result.object as { tools: string[] }).tools.join(", ")}*`,
    );
  };

  const updateSelectedToolsFromMessages = async (messages: UIMessage[]) => {
    const task = messages
      .map(
        (m) =>
          `${m.role}: ${m.parts.map((p) => (p.type === "text" ? p.text : "")).join("")}`,
      )
      .join("\n");
    await updateSelectedTools(task);
  };

  // Create a ref to access latest allTools without recreating transport
  const allToolsRef = useRef<Toolset>(allTools);
  useEffect(() => {
    allToolsRef.current = allTools;
  }, [allTools]);

  // Create transport with dynamic configuration
  const transport = useMemo(() => {
    return new CustomChatTransport({
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      model: openrouterChat as any,
      temperature,
      getTools: async (messages: UIMessage[]) => {
        // Use ref to get the latest allTools
        const currentAllTools = allToolsRef.current;
        console.log("getTools: allTools count:", Object.keys(currentAllTools).length);
        console.log("getTools: dynamicToolset:", dynamicToolset);

        let tools = currentAllTools;
        let hasMentions = false;

        // Check for tool mentions in the last user message
        const lastUserMessage = messages.filter((m) => m.role === "user").pop();
        if (isToolTaggingEnabled && lastUserMessage) {
          const lastUserText = lastUserMessage.parts
            .map((p) => (p.type === "text" ? p.text : ""))
            .join("");
          if (lastUserText) {
            const mentionedIds = parseMentionedTools(lastUserText, mentionTools);
            if (mentionedIds.length > 0) {
              hasMentions = true;
              tools = Object.fromEntries(
                Object.entries(currentAllTools).filter(([_, tool]) => {
                  const toolWithId = tool as CoreTool & { id?: string };
                  return mentionedIds.includes(toolWithId.id || _);
                }),
              );
              console.log("getTools: filtered to mentioned tools:", Object.keys(tools));
            }
          }
        }

        // Handle dynamic toolset if no mentions
        if (!hasMentions && dynamicToolset) {
          console.log("getTools: using dynamic toolset, selectedTools:", selectedTools.current);
          if (selectedTools.current.length === 0) {
            await updateSelectedToolsFromMessages(messages);
            console.log("getTools: after updateSelectedToolsFromMessages, selectedTools:", selectedTools.current);
          }
          tools = Object.fromEntries(
            Object.entries(currentAllTools).filter(([tool]) =>
              selectedTools.current.includes(tool),
            ),
          );
          console.log("getTools: filtered to dynamic tools:", Object.keys(tools));
        }

        console.log("getTools: final tools count:", Object.keys(tools).length);

        // Build system prompt
        let systemPrompt = `You are a helpful assistant that can answer questions and help with tasks.
          When using tools, ensure that the arguments match the provided schema. Note that the schema may update as the conversation progresses.
          The current date is ${new Date().toISOString()}`;

        if (hasMentions) {
          const toolNames = Object.keys(tools).join(", ");
          systemPrompt += `
          The user has specifically selected the following tools for this request: ${toolNames}.
          Please use only these tools to fulfill the request.`;
        } else if (dynamicToolset) {
          systemPrompt += `
          If you are unable to fulfill the user's request with the current set of tools, use the refresh_tools tool to get a new set of tools before saying you can't do it.`;
        }

        return {
          tools: Object.fromEntries(
            Object.entries(tools).map(([name, tool]) => [
              name,
              {
                description: tool.description,
                inputSchema: tool.inputSchema,
              },
            ]),
          ),
          systemPrompt,
        };
      },
      onError: (event: { error: unknown }) => {
        let displayMessage = extractStreamError(event);
        if (displayMessage) {
          if (displayMessage.includes("maximum context length")) {
            const cutoffPhrase = "Please reduce the length of either one";
            const cutoffIndex = displayMessage.indexOf(cutoffPhrase);
            if (cutoffIndex !== -1) {
              displayMessage = displayMessage.substring(0, cutoffIndex);
            }
            displayMessage +=
              " Please start a new chat history and consider enabling *Auto-Summarize* for your tool or revise your prompt.";
          }
          if (displayMessage.includes("requires more credits")) {
            displayMessage =
              "You have reached your monthly credit limit. Reach out to the Speakeasy team to upgrade your account.";
          }
          appendDisplayOnlyMessage(`**Model Error:** *${displayMessage}*`);
        }
      },
    });
  }, [openrouterChat, temperature, allTools, dynamicToolset, isToolTaggingEnabled, mentionTools]);

  const initialMessagesInner: UIMessage[] =
    chatHistory.length > 0 ? chatHistory : (initialMessages ?? []);

  const {
    messages: chatMessages,
    status,
    sendMessage,
    addToolResult,
  } = useChat({
    id: chat.id,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    transport: transport as any,
    onError: (error) => {
      console.error("Chat error:", error.message, error.stack);
      if (error.message.trim() !== "An error occurred.") {
        appendDisplayOnlyMessage(`**Error:** *${error.message}*`);
      }
      telemetry.capture("chat_event", {
        action: "chat_error",
        error: error.message,
      });
    },
    maxSteps: 5,
    initialMessages: initialMessagesInner,
    onToolCall: async ({ toolCall }) => {
      console.log("onToolCall received:", toolCall);
      console.log("onToolCall keys:", Object.keys(toolCall));
      console.log("onToolCall stringified:", JSON.stringify(toolCall, null, 2));
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const toolName = (toolCall as any).toolName;
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const toolArgs = (toolCall as any).args;
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const toolCallId = (toolCall as any).toolCallId;

      console.log("Executing tool:", toolName, "with args:", toolArgs);

      const tool = allToolsRef.current[toolName];

      if (!tool) {
        console.error("Tool not found:", toolName, "Available tools:", Object.keys(allToolsRef.current));
        appendDisplayOnlyMessage(`**Error:** *Tool ${toolName} not found*`);
        return;
      }

      const requiresApproval =
        tool?.method !== "GET" && toolName !== updateToolsTool.name;

      try {
        const result = await executeTool(toolName, toolArgs);
        console.log("Tool result:", result);

        addToolResult({
          toolCallId,
          output: result,
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
        } as any);
      } catch (error) {
        console.error("Tool execution error:", error);
        appendDisplayOnlyMessage(`**Tool Error:** *${error instanceof Error ? error.message : "Unknown error"}*`);
      }
    },
  });

  const executeTool = async (toolName: string, input: unknown) => {
    if (toolName === updateToolsTool.name) {
      return updateToolsTool.execute!(input);
    }

    const tool = allTools[toolName];
    if (!tool) {
      throw new Error(`Tool ${toolName} not found`);
    }

    return await tool.execute!(input);
  };

  const handleSend = useCallback(
    async (msg: string) => {
      const userMessages = chatMessages.filter((m) => m.role === "user");
      if (userMessages.length === 0) {
        telemetry.capture("chat_event", {
          action: "chat_started",
          model,
          message: msg,
        });

        localStorage.setItem(onboardingStepStorageKeys.test, "true");
      }

      if (isToolTaggingEnabled) {
        const mentionedIds = parseMentionedTools(msg, mentionTools);
        if (mentionedIds.length > 0) {
          telemetry.capture("chat_event", {
            action: "tools_mentioned",
            tool_count: mentionedIds.length,
          });
        }
      }

      sendMessage({ text: msg });
      setInputText("");
    },
    [chatMessages, telemetry, model, mentionTools, sendMessage],
  );

  useEffect(() => {
    chat.setAppendMessage((message) => {
      sendMessage({ text: message.content as string });
    });
  }, [sendMessage]);

  useEffect(() => {
    setMessages(chatMessages);
  }, [chatMessages]);

  useEffect(() => {
    setDisplayOnlyMessages([]);
  }, [chat.id]);

  const messagesToDisplay = [...displayOnlyMessages, ...chatMessages];

  const { isNavigating, historyIndex, totalMessages } =
    useMessageHistoryNavigation(chatMessages);

  const temperatureSlider = (
    <div className="flex items-center gap-3 px-2">
      <SimpleTooltip tooltip="Controls randomness in responses. Lower values (0.0-0.3) make outputs more focused and deterministic. Higher values (0.7-1.0) increase creativity and variety. Default: 0.5">
        <span className="text-xs text-muted-foreground whitespace-nowrap cursor-help">
          Temp: {temperature.toFixed(1)}
        </span>
      </SimpleTooltip>
      <Slider
        value={temperature}
        onChange={setTemperature}
        min={0}
        max={1}
        step={0.1}
        className="w-24"
      />
    </div>
  );

  const modelSelector = (
    <Combobox
      items={availableModels.map((m) => ({ label: m.label, value: m.value }))}
      onSelectionChange={(item) => setModel(item.value)}
      selected={model}
      variant="ghost"
      className="w-fit"
    >
      <Stack direction="horizontal" gap={2} align="center">
        <Type variant="small" className="font-medium">
          {availableModels.find((m) => m.value === model)?.label}
        </Type>
      </Stack>
    </Combobox>
  );

  const chatContent = (
    <div className="relative h-full flex flex-col">
      <div className="flex items-center gap-2 border-b px-4 py-2">
        {modelSelector}
        {temperatureSlider}
      </div>
      <ChatMessages
        messages={messagesToDisplay}
        isLoading={status === "streaming" || isChatHistoryLoading}
        className="flex-1"
        renderMessage={(message) => (
          <CustomMessageRenderer
            message={message}
            project={project}
            allTools={allTools}
          />
        )}
      />
      <ChatInput
        onSend={handleSend}
        disabled={status === "streaming"}
        initialInput={initialPrompt || undefined}
        additionalActions={
          <div className="flex items-center gap-2">{additionalActions}</div>
        }
      />
      <MessageHistoryIndicator
        isNavigating={isNavigating}
        historyIndex={historyIndex}
        totalMessages={totalMessages}
      />
    </div>
  );

  return isToolTaggingEnabled ? (
    <ChatComposerWrapper
      tools={mentionTools}
      onToolsSelected={setMentionedToolIds}
      onInputChange={setInputText}
    >
      {chatContent}
    </ChatComposerWrapper>
  ) : (
    chatContent
  );
}

function CustomMessageRenderer({
  message,
  project,
  allTools,
}: {
  message: UIMessage;
  project: { slug: string; id: string };
  allTools: Toolset;
}) {
  const isUser = message.role === "user";

  return (
    <div
      className={cn(
        "flex flex-col gap-2 rounded-lg p-4",
        isUser
          ? "ml-auto max-w-[80%] bg-primary text-primary-foreground"
          : "mr-auto max-w-[80%] bg-muted",
      )}
    >
      <div className="flex items-center gap-2">
        {isUser ? (
          <ProjectAvatar project={project} className="h-6 w-6" />
        ) : (
          <div className="h-6 w-6 rounded-full bg-primary/10 flex items-center justify-center">
            <span className="text-xs">AI</span>
          </div>
        )}
        <Type variant="small" className="font-medium opacity-70">
          {isUser ? "You" : "Assistant"}
        </Type>
      </div>
      <div className="whitespace-pre-wrap">
        {message.parts.map((part, index) => {
          if (part.type === "text") {
            return <span key={index}>{part.text}</span>;
          }
          if (part.type === "tool-call" || part.type === "tool-result") {
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            const toolPart = part as any;
            if (part.type === "tool-call" && toolPart.toolName) {
              const tool = allTools[toolPart.toolName];
              return (
                <div key={index} className="my-2 rounded border p-2 bg-blue-50/50 dark:bg-blue-950/20">
                  <Stack direction="horizontal" gap={2} align="center">
                    <Type variant="small" className="font-medium">
                      🔧 {toolPart.toolName}
                    </Type>
                    {toolPart.state === "input-streaming" && (
                      <span className="text-xs text-muted-foreground">Preparing...</span>
                    )}
                  </Stack>
                  {tool?.method && tool?.path && (
                    <HttpRoute method={tool.method} path={tool.path} />
                  )}
                  {toolPart.args && (
                    <details className="mt-2">
                      <summary className="text-xs font-medium cursor-pointer">Input</summary>
                      <pre className="mt-1 text-xs opacity-70 overflow-auto max-h-40">
                        {JSON.stringify(toolPart.args, null, 2)}
                      </pre>
                    </details>
                  )}
                </div>
              );
            }
            if (part.type === "tool-result" && toolPart.toolName) {
              return (
                <div key={index} className="my-2 rounded border p-2 bg-green-50/50 dark:bg-green-950/20">
                  <Type variant="small" className="font-medium text-green-700 dark:text-green-300">
                    ✓ {toolPart.toolName} result
                  </Type>
                  {toolPart.result && (
                    <details className="mt-2" open>
                      <summary className="text-xs font-medium cursor-pointer">Output</summary>
                      <pre className="mt-1 text-xs opacity-70 overflow-auto max-h-40">
                        {typeof toolPart.result === "string"
                          ? toolPart.result
                          : JSON.stringify(toolPart.result, null, 2)}
                      </pre>
                    </details>
                  )}
                </div>
              );
            }
          }
          return null;
        })}
      </div>
    </div>
  );
}

const extractStreamError = (event: { error: unknown }) => {
  let message: string | undefined;
  if (typeof event.error === "object" && event.error !== null) {
    const errorObject = event.error as {
      responseBody?: unknown;
      message?: unknown;
      [key: string]: unknown;
    };

    if (typeof errorObject.responseBody === "string") {
      try {
        const parsedBody = JSON.parse(errorObject.responseBody);
        if (
          typeof parsedBody === "object" &&
          parsedBody !== null &&
          parsedBody.error
        ) {
          if (parsedBody.error.metadata?.raw) {
            try {
              const rawError = JSON.parse(parsedBody.error.metadata.raw);
              if (rawError.error?.message) {
                message = rawError.error.message;
              }
            } catch (_e) {
              if (typeof parsedBody.error.message === "string") {
                message = parsedBody.error.message;
              }
            }
          } else if (typeof parsedBody.error.message === "string") {
            message = parsedBody.error.message;
          }
        }
      } catch (e) {
        console.error(`Error parsing model error: ${e}`);
      }
    } else if (typeof errorObject.message === "string") {
      message = errorObject.message;
    }
  }

  return message;
};
