import { Page } from "@/components/page-layout";
import { Combobox, DropdownItem } from "@/components/ui/combobox";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useProject, useSession } from "@/contexts/Auth";
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
} from "@speakeasy-api/moonshine";
import { jsonSchema, smoothStream, streamText, ToolInvocation } from "ai";
import { useCallback, useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { v7 as uuidv7 } from "uuid";
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
  const [chatId, setChatId] = useState<string>(uuidv7());

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
    >
      <Stack
        direction="horizontal"
        gap={2}
        align="center"
      >
        <Icon name="history" className="opacity-50" />
        <Type variant="small">Chat History</Type>
      </Stack>
    </Combobox>
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions>{chatHistoryButton}</Page.Header.Actions>
      </Page.Header>
      <Page.Body className="max-w-full">
        <ResizablePanel direction="horizontal" className="h-full">
          <ResizablePanel.Pane minSize={35}>
            <ChatWindow configRef={chatConfigRef} chatId={chatId} />
          </ResizablePanel.Pane>
          <ResizablePanel.Pane minSize={35} order={0}>
            <ToolsetPanel
              configRef={chatConfigRef}
              setSelectedToolset={setSelectedToolset}
              setSelectedEnvironment={setSelectedEnvironment}
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
}: {
  configRef: ChatConfig;
  setSelectedToolset: (toolset: string) => void;
  setSelectedEnvironment: (environment: string) => void;
}) {
  const { data: toolsetsData } = useListToolsets();
  const { data: environmentsData } = useListEnvironments();

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
          </Stack>
          <Stack direction="horizontal" gap={2} align="center">
            <Heading variant="h5">Active environment: </Heading>
            {environmentDropdown}
          </Stack>
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
}: {
  configRef: ChatConfig;
  chatId: string;
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
    />
  );
}

function ChatInner({
  model,
  setModel,
  configRef,
  chatId,
}: {
  model: string;
  setModel: (model: string) => void;
  configRef: ChatConfig;
  chatId: string;
}) {
  const session = useSession();
  const project = useProject();
  const { chatHistory, isLoading: isChatHistoryLoading } =
    useChatHistory(chatId);

  const instance = useInstance(
    {},
    {
      toolsetSlug: configRef.current.toolsetSlug ?? "",
      environmentSlug: configRef.current.environmentSlug ?? undefined,
    },
    {
      enabled:
        !!configRef.current.toolsetSlug && !!configRef.current.environmentSlug,
    }
  );

  const tools = Object.fromEntries(
    instance.data?.tools.map((tool) => {
      return [
        tool.name,
        {
          id: tool.id,
          description: tool.description,
          parameters: jsonSchema(tool.schema ? JSON.parse(tool.schema) : {}),
        },
      ];
    }) ?? []
  );

  const openrouter = createOpenRouter({
    apiKey: "this is required",
    baseURL: getServerURL(),
    headers: {
      "Gram-Session": session.session,
      "Gram-Project": project.slug,
      "Gram-Chat-ID": chatId,
    },
  });

  const openaiFetch: typeof globalThis.fetch = async (_, init) => {
    const result = streamText({
      model: openrouter.chat(model),
      messages: JSON.parse(init?.body as string).messages,
      tools,
      temperature: 0.5,
      system:
        "You are a helpful assistant that can answer questions and help with tasks. The current date is " +
        new Date().toISOString(),
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
    onToolCall: async ({ toolCall }) => {
      const tool = tools[toolCall.toolName];
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

      return result || "";
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

  // TODO: fix this
  /* eslint-disable  @typescript-eslint/no-explicit-any */
  const m = chatMessages as any;

  return (
    <AIChatContainer
      messages={m}
      isLoading={status === "streaming" || isChatHistoryLoading}
      onSendMessage={handleSend}
      className="pb-4"
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
