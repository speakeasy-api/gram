import { AutoSummarizeBadge } from "@/components/auto-summarize-badge";
import { HttpRoute } from "@/components/http-route";
import { ProjectAvatar } from "@/components/project-menu";
import { Link } from "@/components/ui/link";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useProject, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { Telemetry, useTelemetry } from "@/contexts/Telemetry";
import { cn, getServerURL } from "@/lib/utils";
import { Message, useChat } from "@ai-sdk/react";
import { HTTPToolDefinition } from "@gram/client/models/components";
import { useInstance } from "@gram/client/react-query/index.js";
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
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { v7 as uuidv7 } from "uuid";
import { z } from "zod";
import { useChatContext } from "./ChatContext";
import { useChatHistory } from "./ChatHistory";
import { MessageHistoryIndicator } from "./MessageHistoryIndicator";
import { useMiniModel, useModel } from "./Openrouter";
import { useMessageHistoryNavigation } from "./useMessageHistoryNavigation";
import { onboardingStepStorageKeys } from "../home/Home";

const defaultModel = {
  label: "Claude 4 Sonnet",
  value: "anthropic/claude-sonnet-4",
};

const availableModels = [
  defaultModel,
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
  initialMessages?: Message[];
  initialPrompt?: string | null;
}) {
  const [model, setModel] = useState(defaultModel.value);
  const chatKey = `chat-${model}`;

  // We do this because we want the chat to reset when the model changes
  return (
    <ChatInner
      key={chatKey}
      model={model}
      setModel={setModel}
      configRef={configRef}
      dynamicToolset={dynamicToolset}
      initialMessages={initialMessages}
      additionalActions={additionalActions}
      initialPrompt={initialPrompt}
    />
  );
}

type Toolset = Record<string, Tool & { method?: string; path?: string }>;

function ChatInner({
  model,
  setModel,
  configRef,
  dynamicToolset,
  initialMessages,
  additionalActions,
  initialPrompt,
}: {
  model: string;
  setModel: (model: string) => void;
  configRef: ChatConfig;
  dynamicToolset: boolean;
  initialMessages?: Message[];
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
    chat.id
  );

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

  const executeHttpToolFn =
    (tool: HTTPToolDefinition, toolsetSlug: string) =>
    async (args: unknown) => {
      const response = await fetch(
        `${getServerURL()}/rpc/instances.invoke/tool?tool_id=${
          tool.id
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
        }
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
    const tools: Toolset = Object.fromEntries(
      instance.data?.tools.map((tool) => {
        return [
          tool.name,
          {
            method: tool.httpMethod,
            path: tool.path,
            description: tool.description,
            parameters: jsonSchema(tool.schema ? JSON.parse(tool.schema) : {}),
            execute: executeHttpToolFn(
              tool,
              configRef.current.toolsetSlug || ""
            ),
          },
        ];
      }) ?? []
    );

    instance.data?.promptTemplates?.forEach((pt) => {
      tools[pt.name] = {
        description: pt.description ?? "",
        parameters: jsonSchema(JSON.parse(pt.arguments ?? "{}")),
        execute: async (args) => {
          const res = await client.templates.renderByID({
            id: pt.id,
            renderTemplateByIDRequestBody: {
              arguments: args,
            },
          });

          return res.prompt;
        },
      };
    });

    return tools;
  }, [instance.data]);

  const openrouterChat = useModel(model, {
    "Gram-Chat-ID": chat.id,
  });

  const openrouterBasic = useMiniModel();

  const updateToolsTool: Tool & { name: string } = {
    name: "refresh_tools",
    description: `If you are unable to fulfill the user's request with the current set of tools, use this tool to get a new set of tools.
    The request is a description of the task you are trying to complete based on the conversation history. 
    Try to incorporate not just the most recent messages, but also the overall task the user has been trying to accomplish over the course of the chat.`,
    parameters: z.object({
      priorConversationSummary: z.string(),
      previouslyUsedTools: z.array(z.string()),
      newRequest: z.string(),
    }),
    execute: async (args) => {
      const parsedArgs = updateToolsTool.parameters.parse(args);
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
      model: openrouterBasic,
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
      `**Updated tool list:** *${(
        result.object as { tools: string[] }
      ).tools.join(", ")}*`
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
        ])
      ),
      temperature: 0.5,
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
    chatHistory.length > 0 ? chatHistory : initialMessages ?? [];

  const toolCallApproval = useToolCallApproval({
    // Disclaimer: this is a bit weird, because the tool's execute function actually seems to be called by the useChat hook
    executeToolCall: async (toolCall) => {
      if (toolCall.toolName === updateToolsTool.name) {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        return updateToolsTool.execute!(toolCall.args, {} as any);
      }

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
      return toolCall.toolName !== updateToolsTool.name;
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

      await append({
        role: "user",
        content: msg,
      });
    },
    [append, chatMessages, telemetry, model]
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

  return (
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
            additionalActions,
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
}

const toolCallComponents = (tools: Toolset, telemetry: Telemetry) => {
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
