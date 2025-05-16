import { HttpRoute } from "@/components/http-route";
import { Page } from "@/components/page-layout";
import { ProjectAvatar } from "@/components/project-menu";
import { Button } from "@/components/ui/button";
import { Combobox, DropdownItem } from "@/components/ui/combobox";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useIsAdmin, useProject, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { dateTimeFormatters } from "@/lib/dates";
import { capitalize, getServerURL } from "@/lib/utils";
import { Message, useChat } from "@ai-sdk/react";
import { Deployment } from "@gram/client/models/components";
import {
  useInstance,
  useListChats,
  useListEnvironments,
  useListToolsets,
  useLoadChat,
} from "@gram/client/react-query/index.js";
import { createOpenRouter } from "@openrouter/ai-sdk-provider";
import {
  AIChatContainer,
  Icon,
  ResizablePanel,
  Stack,
  useToolCallApproval,
} from "@speakeasy-api/moonshine";
import {
  generateObject,
  jsonSchema,
  smoothStream,
  streamText,
  Tool,
  ToolInvocation,
} from "ai";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { v7 as uuidv7 } from "uuid";
import { z } from "zod";
import { OnboardingContent } from "../onboarding/Onboarding";
import { ToolsetView } from "../toolsets/Toolset";

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

type ChatConfig = React.RefObject<{
  toolsetSlug: string | null;
  environmentSlug: string | null;
  isOnboarding: boolean;
}>;

export default function Playground() {
  const [searchParams] = useSearchParams();
  const { data: chatsData, refetch: refetchChats } = useListChats();

  const [selectedToolset, setSelectedToolset] = useState<string | null>(
    searchParams.get("toolset") ?? null
  );
  const [selectedEnvironment, setSelectedEnvironment] = useState<string | null>(
    searchParams.get("environment") ?? null
  );
  const [dynamicToolset, setDynamicToolset] = useState(false);
  const [chatId, setChatId] = useState<string>(
    searchParams.get("chatId") ?? uuidv7()
  );

  // We use a ref so that we can hot-swap the toolset and environment without causing a re-render
  const chatConfigRef = useRef({
    toolsetSlug: selectedToolset,
    environmentSlug: selectedEnvironment,
    isOnboarding: false,
  });

  chatConfigRef.current = {
    toolsetSlug: selectedToolset,
    environmentSlug: selectedEnvironment,
    isOnboarding: false,
  };

  const chatHistoryItems: DropdownItem[] =
    chatsData?.chats
      .sort((a, b) => b.updatedAt.getTime() - a.updatedAt.getTime())
      .map((chat) => ({
        label: capitalize(dateTimeFormatters.humanize(chat.updatedAt)),
        value: chat.id,
      })) ?? [];

  chatHistoryItems.push({
    icon: <Icon name="plus" />,
    label: "New chat",
    value: uuidv7(),
  });

  const chatHistoryButton = (
    <Combobox
      items={chatHistoryItems}
      onSelectionChange={(item) => {
        setChatId(item.value);
      }}
      selected={chatId}
      variant="ghost"
      onOpenChange={(open) => {
        if (open) {
          refetchChats();
        }
      }}
      className="w-fit"
    >
      <Stack direction="horizontal" gap={2} align="center">
        <Icon name="history" className="opacity-50" />
        <Type variant="small">Chat History</Type>
      </Stack>
    </Combobox>
  );

  const shareChatButton = (
    <Button
      size="sm"
      icon="link"
      variant={"ghost"}
      onClick={() => {
        const url = new URL(window.location.href);
        url.searchParams.set("chatId", chatId);
        navigator.clipboard.writeText(url.toString());
        alert("Chat link copied to clipboard");
      }}
    >
      Share chat
    </Button>
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions className="gap-2">
          {shareChatButton}
          {chatHistoryButton}
        </Page.Header.Actions>
      </Page.Header>
      <Page.Body className="max-w-full">
        <ResizablePanel
          direction="horizontal"
          className="h-full [&>[role='separator']]:border-border"
        >
          <ResizablePanel.Pane minSize={35}>
            <ChatWindow
              configRef={chatConfigRef}
              chatId={chatId}
              dynamicToolset={dynamicToolset}
            />
          </ResizablePanel.Pane>
          <ResizablePanel.Pane minSize={35} order={0}>
            <ToolsetPanel
              configRef={chatConfigRef}
              setSelectedToolset={setSelectedToolset}
              setSelectedEnvironment={setSelectedEnvironment}
              dynamicToolset={dynamicToolset}
              setDynamicToolset={setDynamicToolset}
            />
          </ResizablePanel.Pane>
        </ResizablePanel>
      </Page.Body>
    </Page>
  );
}

export function OnboardingPanel({
  selectToolset,
}: {
  selectToolset: (toolsetSlug: string) => void;
}) {
  const client = useSdkClient();

  const onOnboardingComplete = async (deployment: Deployment) => {
    const assetName = deployment.openapiv3Assets[0]?.name;

    if (!assetName) {
      throw new Error("No asset name found");
    }

    // Auto-create a default toolset
    const res = await client.toolsets.create({
      createToolsetRequestBody: {
        name: assetName,
        description: `A toolset created from OpenAPI document: ${assetName}`,
      },
    });

    const allTools = await client.tools.list();

    // Add all tools to the toolset
    await client.toolsets.updateBySlug({
      slug: res.slug,
      updateToolsetRequestBody: {
        httpToolNames: allTools.tools.map((tool) => tool.name),
      },
    });

    selectToolset(res.slug);
  };

  return <OnboardingContent onOnboardingComplete={onOnboardingComplete} />;
}

export function ToolsetPanel({
  configRef,
  setSelectedToolset,
  setSelectedEnvironment,
  dynamicToolset,
  setDynamicToolset,
}: {
  configRef: ChatConfig;
  setSelectedToolset: (toolset: string) => void;
  setSelectedEnvironment: (environment: string) => void;
  dynamicToolset: boolean;
  setDynamicToolset: (dynamicToolset: boolean) => void;
}) {
  const { data: toolsetsData } = useListToolsets();
  const { data: environmentsData } = useListEnvironments();
  const isAdmin = useIsAdmin();

  const toolsets = toolsetsData?.toolsets;
  const environments = environmentsData?.environments;

  const selectedToolset = configRef.current.toolsetSlug;
  const selectedEnvironment = configRef.current.environmentSlug;

  const toolset = toolsets?.find((toolset) => toolset.slug === selectedToolset);

  useEffect(() => {
    if (toolsets?.[0] && configRef.current.toolsetSlug === null) {
      setSelectedToolset(toolsets[0].slug);
      if (toolsets[0].defaultEnvironmentSlug) {
        setSelectedEnvironment(toolsets[0].defaultEnvironmentSlug);
      }
    }
  }, [toolsets, configRef, setSelectedToolset, setSelectedEnvironment]);

  useEffect(() => {
    if (environments?.[0] && configRef.current.environmentSlug === null) {
      if (toolset?.defaultEnvironmentSlug) {
        setSelectedEnvironment(toolset.defaultEnvironmentSlug);
      } else {
        setSelectedEnvironment(environments[0].slug);
      }
    }
  }, [environments, configRef, setSelectedEnvironment, toolset]);

  useEffect(() => {
    const isDynamic =
      toolset?.httpTools?.length && toolset.httpTools.length > 40 && isAdmin;
    setDynamicToolset(!!isDynamic);
  }, [toolset, isAdmin, setDynamicToolset]);

  const toolsetDropdownItems =
    toolsets?.map((toolset) => ({
      ...toolset,
      label: toolset.name,
      value: toolset.slug,
    })) ?? [];

  const toolsetDropdown = (
    <Combobox
      items={toolsetDropdownItems}
      selected={toolsetDropdownItems.find(
        (item) => item.value === selectedToolset
      )}
      onSelectionChange={(value) => setSelectedToolset(value.value)}
      className="max-w-fit"
    >
      <Type variant="small">{toolset?.name}</Type>
    </Combobox>
  );

  const environmentDropdownItems =
    environments?.map((environment) => ({
      ...environment,
      label: environment.name,
      value: environment.slug,
    })) ?? [];

  const environmentDropdown = (
    <Combobox
      items={environmentDropdownItems}
      selected={environmentDropdownItems.find(
        (item) => item.value === selectedEnvironment
      )}
      onSelectionChange={(value) => setSelectedEnvironment(value.value)}
      className="max-w-fit"
    >
      <Type variant="small">{selectedEnvironment}</Type>
    </Combobox>
  );

  // This is prefetched in PrefetchedQueries, so this state shouldn't be hit
  if (toolsets === undefined) {
    return <div>Loading...</div>;
  }

  // If listToolsets has completed and there's nothing there, show the onboarding panel
  if (toolsets !== undefined && !configRef.current.toolsetSlug) {
    configRef.current.isOnboarding = true;
    return <OnboardingPanel selectToolset={setSelectedToolset} />;
  }

  return (
    <div className="max-h-full overflow-scroll rounded-tr-xl relative">
      <div className="sticky top-0 bg-card py-3 px-8 border-b z-10">
        <Stack direction="horizontal" gap={2} justify="space-between">
          <Stack direction="horizontal" gap={2} align="center">
            <Heading variant="h5">Active toolset: </Heading>
            {toolsetDropdown}
            {isAdmin && (
              <Button
                variant="ghost"
                icon={dynamicToolset ? "sparkles" : "lock"}
                onClick={() => setDynamicToolset(!dynamicToolset)}
                tooltip={
                  dynamicToolset
                    ? "Make the toolset static (use every tool in the toolset)"
                    : "Make the toolset dynamic (use only relevant tools)"
                }
              >
                {dynamicToolset ? "Dynamic" : "Static"}
              </Button>
            )}
          </Stack>
          {environmentDropdownItems.length > 1 && (
            <Stack direction="horizontal" gap={2} align="center">
              <Heading variant="h5">Active environment: </Heading>
              {environmentDropdown}
            </Stack>
          )}
        </Stack>
      </div>
      <ToolsetView
        toolsetSlug={selectedToolset ?? ""}
        className="p-8 2xl:p-12"
        environmentSlug={selectedEnvironment ?? undefined}
      />
    </div>
  );
}

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
  const { chatHistory, isLoading: isChatHistoryLoading } =
    useChatHistory(chatId);

  const selectedTools = useRef<Toolset>({});

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

  const updateSelectedTools = async (task: string): Promise<Toolset> => {
    if (Object.keys(allTools).length === 0) return {};

    if (!dynamicToolset) {
      return allTools;
    }

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

    const filteredTools = Object.fromEntries(
      Object.entries(allTools).filter(([toolName]) =>
        result.object.tools.includes(toolName)
      )
    );

    if (filteredTools[updateToolsTool.name]) {
      throw new Error("update_tools tool already exists");
    }

    filteredTools[updateToolsTool.name] = updateToolsTool;

    selectedTools.current = filteredTools;

    return filteredTools;
  };

  const updateSelectedToolsFromMessages = async (
    messages: Message[]
  ): Promise<Toolset> => {
    const task = messages.map((m) => `${m.role}: ${m.content}`).join("\n");
    return updateSelectedTools(task);
  };

  const openaiFetch: typeof globalThis.fetch = async (_, init) => {
    const messages = JSON.parse(init?.body as string).messages;

    let tools = dynamicToolset ? selectedTools.current : allTools;

    // On the first message, get the initial set of tools if we're using a dynamic toolset
    if (dynamicToolset && Object.keys(tools).length === 0) {
      tools = await updateSelectedToolsFromMessages(messages);
    }

    let systemPrompt = `You are a helpful assistant that can answer questions and help with tasks.
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
        const result = await updateSelectedTools(JSON.stringify(args));
        return `Updated tool list: ${Object.keys(result).join(", ")}`;
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

      const result = await response.json();

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

  const {
    messages: chatMessages,
    status,
    append,
  } = useChat({
    id: chatId,
    fetch: openaiFetch,
    onError: (error) => {
      console.error("Chat error:", error.message, error.stack);
    },
    maxSteps: 5,
    initialMessages,
    onToolCall: toolCallApproval.toolCallFn,
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

  const JsonDisplay = ({ json }: { json: string }) => {
    return (
      <pre className="typography-body-xs max-h-48 overflow-auto rounded bg-neutral-900 p-2 break-all whitespace-pre-wrap text-neutral-300">
        {json}
      </pre>
    );
  };

  const toolCallComponents = {
    input: (props: { toolName: string; args: string }) => {
      const tool = allTools[props.toolName];
      return (
        <Stack gap={2}>
          {tool?.method && tool?.path && (
            <HttpRoute method={tool.method} path={tool.path} />
          )}
          <JsonDisplay json={props.args} />
        </Stack>
      );
    },
    result: (props: { toolName: string; result: string }) => {
      const tool = allTools[props.toolName];
      return (
        <Stack gap={2}>
          <Type variant="small" className="font-medium text-muted-foreground">
            {tool?.method ? "Response Body" : "Result"}
          </Type>
          <JsonDisplay json={props.result} />
        </Stack>
      );
    },
  };

  // TODO: fix this
  /* eslint-disable  @typescript-eslint/no-explicit-any */
  const m = chatMessages as any;

  return (
    <AIChatContainer
      messages={m}
      isLoading={status === "streaming" || isChatHistoryLoading}
      onSendMessage={handleSend}
      className={"pb-4"}
      toolCallApproval={toolCallApproval}
      components={{
        message: {
          avatar: {
            user: () => <ProjectAvatar project={project} className="h-6 w-6" />,
          },
          toolCall: toolCallComponents,
        },
      }}
      modelSelector={{
        model,
        onModelChange: setModel,
        availableModels,
      }}
    />
  );
}

const useChatHistory = (chatId: string) => {
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
