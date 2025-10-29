import { AutoSummarizeBadge } from "@/components/auto-summarize-badge";
import { HttpRoute } from "@/components/http-route";
import { ProjectAvatar } from "@/components/project-menu";
import { Link } from "@/components/ui/link";
import { Slider } from "@/components/ui/slider";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useProject, useSession } from "@/contexts/Auth";
import { Telemetry, useTelemetry } from "@/contexts/Telemetry";
import { asTool, Tool } from "@/lib/toolTypes";
import { cn, getServerURL } from "@/lib/utils";
import { Message, useChat } from "@ai-sdk/react";
import { useInstance } from "@gram/client/react-query/index.js";
import {
  AIChatContainer,
  Stack,
  useToolCallApproval,
} from "@speakeasy-api/moonshine";
import {
  Tool as AiSdkTool,
  jsonSchema,
  smoothStream,
  streamText,
  ToolCall,
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

const defaultModel = {
  label: "Claude 4.5 Sonnet",
  value: "anthropic/claude-sonnet-4.5",
};

const availableModels = [
  defaultModel,
  { label: "Claude 4.5 Haiku", value: "anthropic/claude-haiku-4.5" },
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
  additionalActions,
  initialMessages,
  initialPrompt,
  hideTemperatureSlider = false,
}: {
  configRef: ChatConfig;
  additionalActions?: React.ReactNode;
  initialMessages?: Message[];
  initialPrompt?: string | null;
  hideTemperatureSlider?: boolean;
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
      configRef={configRef}
      initialMessages={initialMessages}
      additionalActions={additionalActions}
      initialPrompt={initialPrompt}
      {...(!hideTemperatureSlider && { temperature, setTemperature })}
    />
  );
}

type AiSdkToolset = Record<
  string,
  AiSdkTool & { urn: string; method?: string; path?: string }
>;

function ChatInner({
  model,
  setModel,
  temperature,
  setTemperature,
  configRef,
  initialMessages,
  additionalActions,
  initialPrompt,
}: {
  model: string;
  setModel: (model: string) => void;
  temperature?: number;
  setTemperature?: (temperature: number) => void;
  configRef: ChatConfig;
  initialMessages?: Message[];
  additionalActions?: React.ReactNode;
  initialPrompt?: string | null;
}) {
  const session = useSession();
  const project = useProject();
  const telemetry = useTelemetry();

  const chat = useChatContext();
  const { setMessages } = chat;
  const { chatHistory, isLoading: isChatHistoryLoading } = useChatHistory(
    chat.id,
  );

  const [displayOnlyMessages, setDisplayOnlyMessages] = useState<Message[]>([]);
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
    [],
  );

  const executeTool =
    (tool: Tool, toolsetSlug: string) => async (args: unknown) => {
      const response = await fetch(
        `${getServerURL()}/rpc/instances.invoke/tool?tool_urn=${
          tool.toolUrn
        }&environment_slug=${configRef.current.environmentSlug}&chat_id=${
          chat.id
        }&toolset_slug=${toolsetSlug}`,
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

  const allTools: AiSdkToolset = useMemo(() => {
    const baseTools = instance.data?.tools.map(asTool);

    const tools: AiSdkToolset = Object.fromEntries(
      baseTools?.map((tool) => {
        return [
          tool.name,
          {
            urn: tool.toolUrn,
            description: tool.description,
            parameters: jsonSchema(tool.schema ? JSON.parse(tool.schema) : {}),
            execute: executeTool(tool, configRef.current.toolsetSlug || ""),
            ...(tool.type === "http"
              ? {
                  method: tool.httpMethod,
                  path: tool.path,
                }
              : {}),
          },
        ];
      }) ?? [],
    );

    return tools;
  }, [instance.data]);

  // Create a list of tools for the mention system
  const mentionTools: MentionTool[] = useMemo(() => {
    return Object.entries(allTools).map(([name, tool]) => {
      const toolWithId = tool as AiSdkTool & { id?: string };
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

  const openaiFetch: typeof globalThis.fetch = async (_, init) => {
    const messages = JSON.parse(init?.body as string).messages;

    let tools = allTools;

    // Check if there are mentioned tools in the message (only if feature flag is enabled)
    const lastUserMessage = messages
      .filter((m: Message) => m.role === "user")
      .pop();
    let hasMentions = false;

    if (isToolTaggingEnabled && lastUserMessage && lastUserMessage.content) {
      const mentionedIds = parseMentionedTools(
        lastUserMessage.content,
        mentionTools,
      );
      if (mentionedIds.length > 0) {
        hasMentions = true;
        // Filter tools to only include mentioned ones
        tools = Object.fromEntries(
          Object.entries(allTools).filter(([_, tool]) => {
            const toolWithId = tool as AiSdkTool & { id?: string };
            return mentionedIds.includes(toolWithId.id || _);
          }),
        );

        // Remove @ mentions from the message before sending to the model
        const cleanedContent = lastUserMessage.content
          .replace(/@\w+\s*/g, "")
          .trim();
        lastUserMessage.content = cleanedContent;
      }
    }

    let systemPrompt = `You are a helpful assistant that can answer questions and help with tasks.
        When using tools, ensure that the arguments match the provided schema. Note that the schema may update as the conversation progresses.
        The current date is ${new Date().toISOString()}`;

    if (hasMentions) {
      const toolNames = Object.keys(tools).join(", ");
      systemPrompt += `
        The user has specifically selected the following tools for this request: ${toolNames}.
        Please use only these tools to fulfill the request.`;
    }

    const result = streamText({
      model: openrouterChat,
      messages,
      tools: Object.fromEntries(
        Object.entries(tools).map(([name, tool]) => [
          name,
          {
            description: tool.description,
            parameters: tool.parameters,
            // Remove execute function - we handle execution in onToolCall
          },
        ]),
      ),
      temperature,
      system: systemPrompt,
      experimental_transform: smoothStream({
        delayInMs: 15, // Looks a little smoother
      }),
      onError: (event: { error: unknown }) => {
        let displayMessage = extractStreamError(event);
        if (displayMessage) {
          // some manipulation to promote summarization
          if (displayMessage.includes("maximum context length")) {
            const cutoffPhrase = "Please reduce the length of either one";
            const cutoffIndex = displayMessage.indexOf(cutoffPhrase);
            if (cutoffIndex !== -1) {
              displayMessage = displayMessage.substring(0, cutoffIndex);
            }
            displayMessage +=
              " Please start a new chat history and consider enabling *Auto-Summarize* for your tool or revise your prompt.";
          }

          // Improve the error message for the case where the model is out of credits
          if (displayMessage.includes("requires more credits")) {
            displayMessage =
              "You have reached your monthly credit limit. Reach out to the Speakeasy team to upgrade your account.";
          }

          appendDisplayOnlyMessage(`**Model Error:** *${displayMessage}*`);
        }
      },
    });

    return result.toDataStreamResponse();
  };

  const initialMessagesInner: Message[] =
    chatHistory.length > 0 ? chatHistory : (initialMessages ?? []);

  const toolCallApproval = useToolCallApproval({
    // Disclaimer: this is a bit weird, because the tool's execute function actually seems to be called by the useChat hook
    executeToolCall: async (toolCall) => {
      const tool = allTools[toolCall.toolName];
      if (!tool) {
        throw new Error(`Tool ${toolCall.toolName} not found`);
      }

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      return await tool.execute!(toolCall.args, {} as any);
    },
    requiresApproval: (toolCall) => {
      const tool = allTools[toolCall.toolName];
      if (tool?.method === "GET") {
        return false;
      }
      return true;
    },
  });

  const validateArgs = (_toolCall: ToolCall<string, unknown>) => {
    // This is stubbed out at this time because we validate args on the backend
    return null;
  };

  const {
    messages: chatMessages,
    status,
    append,
  } = useChat({
    id: chat.id,
    fetch: openaiFetch,
    onError: (error) => {
      console.error("Chat error:", error.message, error.stack);
      // don't write display message for non useful obscured onChat error. StreamText will handle it if it's a model error.
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
      const userMessages = chatMessages.filter((m) => m.role === "user");
      // Capture chat_started event when the user sends their first message
      if (userMessages.length === 0) {
        telemetry.capture("chat_event", {
          action: "chat_started",
          model,
          message: msg,
        });

        localStorage.setItem(onboardingStepStorageKeys.test, "true");
      }

      // Track if tools were mentioned (only if feature flag is enabled)
      if (isToolTaggingEnabled) {
        const mentionedIds = parseMentionedTools(msg, mentionTools);
        if (mentionedIds.length > 0) {
          telemetry.capture("chat_event", {
            action: "tools_mentioned",
            tool_count: mentionedIds.length,
          });
        }
      }

      await append({
        role: "user",
        content: msg,
      });

      // Clear the input text after sending
      setInputText("");
    },
    [append, chatMessages, telemetry, model, mentionTools],
  );

  // This needs to be set so that the chat provider can append messages
  useEffect(() => {
    chat.setAppendMessage(append);
  }, []);

  useEffect(() => {
    setMessages(chatMessages);
  }, [chatMessages]);

  // If chatId changes, clear the display only messages
  useEffect(() => {
    setDisplayOnlyMessages([]);
  }, [chat.id]);

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

  // Enable message history navigation with up/down arrow keys
  const { isNavigating, historyIndex, totalMessages } =
    useMessageHistoryNavigation(chatMessages);

  // TODO: fix this
  /* eslint-disable  @typescript-eslint/no-explicit-any */
  const m = messagesToDisplay as any;

  const temperatureSlider = temperature && setTemperature && (
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

  const chatContent = (
    <div className="relative h-full flex items-center justify-center">
      <AIChatContainer
        messages={m}
        isLoading={status === "streaming" || isChatHistoryLoading}
        onSendMessage={handleSend}
        className={"pb-4 w-3xl"} // Set width explicitly or else it will shrink to the size of the messages
        toolCallApproval={toolCallApproval}
        initialInput={initialPrompt || undefined}
        components={{
          composer: {
            additionalActions: (
              <div className="flex items-center gap-2">
                {temperatureSlider}
                {additionalActions}
              </div>
            ),
            modelSelector: "text-foreground",
          },
          message: {
            avatar: {
              user: () => (
                <ProjectAvatar project={project} className="h-6 w-6" />
              ),
            },
            toolCall: toolCallComponents(allTools, telemetry),
          },
        }}
        modelSelector={{
          model,
          onModelChange: setModel,
          availableModels,
        }}
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

const toolCallComponents = (tools: AiSdkToolset, telemetry: Telemetry) => {
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
      args?: Record<string, unknown>;
    }) => {
      const hasSummary = JSON.stringify(args)?.includes("gram-request-summary");
      const validationError =
        typeof result === "string" &&
        result.includes("Schema validation error");

      const isTooLong =
        typeof result === "string" && result.includes("Response is too long");

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
              validationError && "line-through text-muted-foreground",
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
          {isTooLong && (
            <SimpleTooltip tooltip="Response is too long and has been truncated to avoid bricking your playground's context window. Consider using reponse filtering (click to learn more).">
              <Link
                to="https://docs.getgram.ai/concepts/openapi#response-filtering"
                target="_blank"
                onClick={() => {
                  telemetry.capture("feature_requested", {
                    action: "response_filtering",
                  });
                }}
              >
                <Type variant="small" muted>
                  (truncated)
                </Type>
              </Link>
            </SimpleTooltip>
          )}
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
      args?: Record<string, unknown>;
    }) => {
      const tool = tools[props.toolName];
      const hasSummary = JSON.stringify(props.args)?.includes(
        "gram-request-summary",
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
          // Try to extract the raw error message which contains the actual error
          if (parsedBody.error.metadata?.raw) {
            try {
              const rawError = JSON.parse(parsedBody.error.metadata.raw);
              if (rawError.error?.message) {
                message = rawError.error.message;
              }
              // eslint-disable-next-line @typescript-eslint/no-unused-vars
            } catch (e) {
              // If raw parsing fails, fall back to the main error message
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
