import {
  Conversation,
  ConversationContent,
  ConversationScrollButton,
} from "@/components/ai-elements/conversation";
import { Message, MessageContent } from "@/components/ai-elements/message";
import {
  PromptInput,
  PromptInputBody,
  PromptInputFooter,
  PromptInputSubmit,
  PromptInputTextarea,
  PromptInputTools,
} from "@/components/ai-elements/prompt-input";
import { Response } from "@/components/ai-elements/response";
import {
  ToolContent,
  Tool as ToolElement,
  ToolHeader,
  ToolInput,
  ToolOutput,
} from "@/components/ai-elements/tool";
import { HttpRoute } from "@/components/http-route";
import { useProject, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { CustomChatTransport } from "@/lib/CustomChatTransport";
import {
  asTools,
  filterFunctionTools,
  filterHttpTools,
  filterPromptTools,
} from "@/lib/toolTypes";
import { getServerURL } from "@/lib/utils";
import { useChat } from "@ai-sdk/react";
import { useInstance } from "@gram/client/react-query/index.js";
import {
  jsonSchema,
  lastAssistantMessageIsCompleteWithToolCalls,
  UIMessage,
} from "ai";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { v7 as uuidv7 } from "uuid";
import { onboardingStepStorageKeys } from "../home/Home";
import { ChatComposerWrapper } from "./ChatComposerWrapper";
import { useChatContext } from "./ChatContext";
import { useChatHistory } from "./ChatHistory";
import { MessageHistoryIndicator } from "./MessageHistoryIndicator";
import { useModel } from "./Openrouter";
import { Tool as MentionTool, parseMentionedTools } from "./ToolMentions";
import { useMessageHistoryNavigation } from "./useMessageHistoryNavigation";

type CoreTool = {
  description?: string;
  inputSchema: unknown;
  execute?: (input: unknown) => unknown | Promise<unknown>;
};

const defaultModel = {
  label: "Claude 4.5 Sonnet",
  value: "anthropic/claude-sonnet-4.5",
};

export type ChatConfig = React.RefObject<{
  toolsetSlug: string | null;
  environmentSlug: string | null;
}>;

const MAX_TOOL_RESPONSE_LENGTH = 50_000; // Characters

export function ChatWindow({
  configRef,
  additionalActions,
  initialMessages,
  initialPrompt,
  hideTemperatureSlider = false,
  initialTemperature = 0.5,
  initialModel = defaultModel.value,
  initialMaxTokens = 4096,
  authWarning,
}: {
  configRef: ChatConfig;
  additionalActions?: React.ReactNode;
  initialMessages?: UIMessage[];
  initialPrompt?: string | null;
  hideTemperatureSlider?: boolean;
  initialTemperature?: number;
  initialModel?: string;
  initialMaxTokens?: number;
  authWarning?: React.ReactNode;
}) {
  const [model, setModel] = useState(initialModel);
  const [temperature, setTemperature] = useState(initialTemperature);
  const [maxTokens, setMaxTokens] = useState(initialMaxTokens);
  const chatKey = `chat-${model}`;

  // Sync props with state
  useEffect(() => {
    setTemperature(initialTemperature);
  }, [initialTemperature]);

  useEffect(() => {
    setModel(initialModel);
  }, [initialModel]);

  useEffect(() => {
    setMaxTokens(initialMaxTokens);
  }, [initialMaxTokens]);

  // We do this because we want the chat to reset when the model changes
  return (
    <ChatInner
      key={chatKey}
      model={model}
      setModel={setModel}
      configRef={configRef}
      initialMessages={initialMessages}
      additionalActions={additionalActions}
      initialPrompt={initialPrompt}
      maxTokens={maxTokens}
      authWarning={authWarning}
      {...(!hideTemperatureSlider && { temperature, setTemperature })}
    />
  );
}

type AiSdkToolset = Record<
  string,
  CoreTool & { id?: string; method?: string; path?: string }
>;

function ChatInner({
  model,
  setModel: _setModel,
  temperature: _temperature,
  setTemperature: _setTemperature,
  maxTokens,
  configRef,
  initialMessages: _initialMessages,
  additionalActions,
  initialPrompt: _initialPrompt,
  authWarning,
}: {
  model: string;
  setModel: (model: string) => void;
  temperature?: number;
  setTemperature?: (temperature: number) => void;
  maxTokens: number;
  configRef: ChatConfig;
  initialMessages?: UIMessage[];
  additionalActions?: React.ReactNode;
  initialPrompt?: string | null;
  authWarning?: React.ReactNode;
}) {
  const session = useSession();
  const project = useProject();
  const telemetry = useTelemetry();

  const chat = useChatContext();
  const { setMessages } = chat;
  const { chatHistory, isLoading: isChatHistoryLoading } = useChatHistory(
    chat.id,
  );

  const [displayOnlyMessages, setDisplayOnlyMessages] = useState<UIMessage[]>(
    [],
  );
  const [_mentionedToolIds, setMentionedToolIds] = useState<string[]>([]);
  const [_inputText, setInputText] = useState("");

  // Track which chat ID we've loaded messages for to prevent re-loading
  const loadedChatIdRef = useRef<string | null>(null);

  // Feature flag for experimental tool tagging syntax
  const isToolTaggingEnabled = telemetry.isFeatureEnabled(
    "gram-experimental-chat",
  );

  const instance = useInstance(
    {
      toolsetSlug: configRef.current.toolsetSlug ?? "",
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

  const createToolExecutor =
    (tool: { toolUrn: string }, toolsetSlug: string) =>
    async (args: unknown) => {
      const response = await fetch(
        `${getServerURL()}/rpc/instances.invoke/tool?tool_urn=${tool.toolUrn}&environment_slug=${
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

  const client = useSdkClient();

  const allTools: AiSdkToolset = useMemo(() => {
    const baseTools = asTools(instance.data?.tools ?? []);

    const tools: AiSdkToolset = Object.fromEntries(
      filterHttpTools(baseTools)?.map((tool) => {
        return [
          tool.name,
          {
            id: tool.id,
            description: tool.description,
            inputSchema: jsonSchema(tool.schema ? JSON.parse(tool.schema) : {}),
            execute: createToolExecutor(
              tool,
              configRef.current.toolsetSlug || "",
            ),
            method: tool.httpMethod,
            path: tool.path,
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

    // Add function tools
    filterFunctionTools(baseTools).forEach((ft) => {
      tools[ft.name] = {
        id: ft.id,
        description: ft.description ?? "",
        inputSchema: jsonSchema(ft.schema ? JSON.parse(ft.schema) : {}),
        execute: createToolExecutor(
          { toolUrn: ft.toolUrn },
          configRef.current.toolsetSlug || "",
        ),
      };
    });

    return tools;
  }, [instance.data, client]);

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
    "X-Gram-Source": "elements",
  });

  // Create a ref to access latest allTools without recreating transport
  const allToolsRef = useRef<AiSdkToolset>(allTools);
  useEffect(() => {
    allToolsRef.current = allTools;
  }, [allTools]);

  // Create transport with dynamic configuration
  const transport = useMemo(() => {
    return new CustomChatTransport({
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      model: openrouterChat as any,
      temperature: _temperature ?? 0.5,
      maxGeneratedTokens: maxTokens,
      getTools: async (messages: UIMessage[]) => {
        // Use ref to get the latest allTools
        const currentAllTools = allToolsRef.current;

        let tools = currentAllTools;
        let hasMentions = false;

        // Check for tool mentions in the last user message
        const lastUserMessage = messages.filter((m) => m.role === "user").pop();
        if (isToolTaggingEnabled && lastUserMessage) {
          const lastUserText = lastUserMessage.parts
            .map((p) => (p.type === "text" ? p.text : ""))
            .join("");
          if (lastUserText) {
            const mentionedIds = parseMentionedTools(
              lastUserText,
              mentionTools,
            );
            if (mentionedIds.length > 0) {
              hasMentions = true;
              tools = Object.fromEntries(
                Object.entries(currentAllTools).filter(([_, tool]) => {
                  const toolWithId = tool as CoreTool & { id?: string };
                  return mentionedIds.includes(toolWithId.id || _);
                }),
              );
            }
          }
        }

        // Build system prompt
        let systemPrompt = `You are operating in the context of a product that helps developers create tools for their own MCP servers. When
        choosing how to respond, keep in mind that the user is almost certainly intending to test or interact with one of the available tools.
        Prefer to use one of those tools wherever possible to resolve the prompt.

        When using tools, ensure that the arguments match the provided schema. Note that the schema may update as the conversation progresses.

        If a tool fails due to authentication, ask the user to ensure they have provided the correct credentials in the Auth tab.

        The current date is ${new Date().toISOString()}`;

        if (hasMentions) {
          const toolNames = Object.keys(tools).join(", ");
          systemPrompt += `
          The user has specifically selected the following tools for this request: ${toolNames}.
          Please use only these tools to fulfill the request.`;
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
  }, [
    openrouterChat,
    _temperature,
    allTools,
    isToolTaggingEnabled,
    mentionTools,
    appendDisplayOnlyMessage,
  ]);

  const {
    messages: chatMessages,
    status,
    sendMessage,
    addToolResult,
    setMessages: setUseChatMessages,
  } = useChat({
    // Include model in the chat ID to force a fresh session when switching models
    id: `${chat.id}-${model}`,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    transport: transport as any,
    // Automatically continue conversation when all tool calls are complete
    sendAutomaticallyWhen: lastAssistantMessageIsCompleteWithToolCalls,
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
    onToolCall: async ({ toolCall }) => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const toolName = (toolCall as any).toolName;
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const toolArgs = (toolCall as any).input; // AI SDK 5 uses 'input' not 'args'
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const toolCallId = (toolCall as any).toolCallId;

      const tool = allToolsRef.current[toolName];

      if (!tool) {
        appendDisplayOnlyMessage(`**Error:** *Tool ${toolName} not found*`);
        return;
      }

      try {
        const result = await tool.execute!(toolArgs);

        addToolResult({
          toolCallId,
          output: result,
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
        } as any);
      } catch (error) {
        appendDisplayOnlyMessage(
          `**Tool Error:** *${error instanceof Error ? error.message : "Unknown error"}*`,
        );
      }
    },
  });

  // Load chat history when available (AI SDK 5 workaround)
  // Bridge between React Query (server state) and AI SDK (local state)
  const currentChatId = `${chat.id}-${model}`;
  useEffect(() => {
    // Only load once per chat ID after React Query finishes loading
    if (loadedChatIdRef.current !== currentChatId && !isChatHistoryLoading) {
      loadedChatIdRef.current = currentChatId;

      // Priority: loaded chat history > programmatically provided initial messages
      const initialMessagesInner =
        chatHistory.length > 0 ? chatHistory : _initialMessages;
      if (initialMessagesInner && initialMessagesInner.length > 0) {
        setUseChatMessages(initialMessagesInner);
      }
    }
  }, [currentChatId, isChatHistoryLoading]);

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

  const chatContent = (
    <div className="relative h-full flex flex-col">
      <Conversation className="flex-1">
        <ConversationContent>
          {messagesToDisplay.map((message) => (
            <CustomMessageRenderer
              key={message.id}
              message={message}
              allTools={allTools}
            />
          ))}
        </ConversationContent>
        <ConversationScrollButton />
      </Conversation>
      <div className="w-full px-4 pb-4">
        {authWarning}
        <PromptInput
          onSubmit={(message) => {
            if (message.text) {
              handleSend(message.text);
            }
          }}
        >
          <PromptInputBody>
            <PromptInputTextarea placeholder="Send a message..." />
          </PromptInputBody>
          <PromptInputFooter className="bg-secondary border-t border-neutral-softest rounded-bl-lg rounded-br-lg">
            <PromptInputTools>{additionalActions}</PromptInputTools>
            <PromptInputSubmit
              disabled={status === "streaming"}
              status={status}
            />
          </PromptInputFooter>
        </PromptInput>
      </div>
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
  allTools,
}: {
  message: UIMessage;
  allTools: AiSdkToolset;
}) {
  return (
    <Message from={message.role}>
      <MessageContent variant="flat">
        {message.parts.map((part, index) => {
          if (part.type === "text") {
            return <Response key={index}>{part.text}</Response>;
          }

          // Handle tool invocations
          // AI SDK creates ToolUIPart with type "tool-{toolName}" when tools are passed as a typed object
          if (part.type.startsWith("tool-")) {
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            const toolPart = part as any;
            const toolName = part.type.replace("tool-", "");
            const tool = allTools[toolName];

            return (
              <ToolElement key={index} defaultOpen>
                <ToolHeader
                  title={toolName}
                  type={part.type as `tool-${string}`}
                  state={toolPart.state}
                />
                <ToolContent>
                  {tool?.method && tool?.path && (
                    <div className="px-3 pb-2">
                      <HttpRoute method={tool.method} path={tool.path} />
                    </div>
                  )}
                  {toolPart.input && <ToolInput input={toolPart.input} />}
                  {toolPart.output !== undefined && (
                    <ToolOutput
                      output={toolPart.output}
                      errorText={toolPart.errorText}
                    />
                  )}
                </ToolContent>
              </ToolElement>
            );
          }

          return null;
        })}
      </MessageContent>
    </Message>
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
