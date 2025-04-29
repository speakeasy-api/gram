import { Page } from "@/components/page-layout";
import { Message, useChat } from "@ai-sdk/react";
import { useCallback, useState, useRef, useEffect } from "react";
import {
  AIChatContainer,
  ResizablePanel,
  Stack,
} from "@speakeasy-api/moonshine";
import { useProject, useSession } from "@/contexts/Auth";
import { smoothStream, streamText } from "ai";
import { createOpenAI } from "@ai-sdk/openai";
import {
  useInstance,
  useListToolsets,
  useListEnvironments,
} from "@gram/client/react-query/index.js";
import { jsonSchema } from "ai";
import { useSearchParams } from "react-router-dom";
import { Type } from "@/components/ui/type";
import { Heading } from "@/components/ui/heading";
import { Combobox } from "@/components/ui/combobox";
import { ChevronDownIcon } from "lucide-react";
import { ToolsetView } from "../toolsets/Toolset";
import { OnboardingContent } from "../onboarding/Onboarding";
import { useSdkClient } from "@/contexts/Sdk";
import { Deployment } from "@gram/client/models/components";
import { getServerURL } from "@/lib/utils";

type ChatConfig = React.RefObject<{
  toolsetSlug: string | null;
  environmentSlug: string | null;
  isOnboarding: boolean;
}>;

export default function Sandbox() {
  const [searchParams] = useSearchParams();

  const [selectedToolset, setSelectedToolset] = useState<string | null>(
    searchParams.get("toolset") ?? null
  );
  const [selectedEnvironment, setSelectedEnvironment] = useState<string | null>(
    searchParams.get("environment") ?? null
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

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body className="max-w-full">
        <ResizablePanel direction="horizontal" className="h-full">
          <ResizablePanel.Pane minSize={35}>
            <ChatWindow configRef={chatConfigRef} />
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
  const project = useProject();
  const client = useSdkClient();

  const onOnboardingComplete = async (deployment: Deployment) => {
    const assetName = deployment.openapiv3Assets[0]?.name;

    if (!assetName) {
      throw new Error("No asset name found");
    }

    // Auto-create a default toolset
    const res = await client.toolsets.create({
      gramProject: project.slug,
      createToolsetRequestBody: {
        name: assetName,
        description: `A toolset created from OpenAPI document: ${assetName}`,
      },
    });

    const allTools = await client.tools.list({
      gramProject: project.slug,
    });

    // Add all tools to the toolset
    await client.toolsets.updateBySlug({
      gramProject: project.slug,
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
  console.log("selectedToolset", selectedToolset);

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
      <Stack direction="horizontal" gap={2} align="center">
        <Type variant="small">{toolset?.name}</Type>
        <ChevronDownIcon className="h-4 w-4" />
      </Stack>
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
      <Stack direction="horizontal" gap={2} align="center">
        <Type variant="small">{selectedEnvironment}</Type>
        <ChevronDownIcon className="h-4 w-4" />
      </Stack>
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

export function ChatWindow({ configRef }: { configRef: ChatConfig }) {
  const session = useSession();
  const project = useProject();

  const instance = useInstance(
    {},
    {
      gramProject: project.slug,
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

  const openai = createOpenAI({
    apiKey: "this is required",
    baseURL: getServerURL(),
    headers: {
      "Gram-Session": session.session,
    },
  });

  const openaiFetch: typeof globalThis.fetch = async (_, init) => {
    const result = streamText({
      model: openai("gpt-4o"),
      messages: JSON.parse(init?.body as string).messages,
      tools,
      temperature: 0.5,
      system:
        "You are a helpful assistant that can answer questions and help with tasks. The current date is " +
        new Date().toISOString(),
      experimental_transform: smoothStream({
        delayInMs: 20, // Looks a little smoother
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
    : [];

  const { messages, status, append } = useChat({
    fetch: openaiFetch,
    onFinish: (message) => {
      console.log("Chat finished with message:", message);
    },
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

      console.log("Received new tool call:", toolCall);

      const response = await fetch(
        `${getServerURL()}/rpc/instances.invoke/tool?tool_id=${tool.id}&environment_slug=${configRef.current.environmentSlug}`,
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

      console.log("tool result", result);

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
  const m = messages as any;
  return (
    <AIChatContainer
      messages={m}
      isLoading={status === "streaming"}
      onSendMessage={handleSend}
      className="pb-4"
    />
  );
}
