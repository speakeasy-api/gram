import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { Combobox, DropdownItem } from "@/components/ui/combobox";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useIsAdmin } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import {
  useRegisterEnvironmentTelemetry,
  useRegisterToolsetTelemetry,
  useTelemetry,
} from "@/contexts/Telemetry";
import { dateTimeFormatters } from "@/lib/dates";
import { capitalize, cn } from "@/lib/utils";
import { Deployment } from "@gram/client/models/components";
import {
  useListChats,
  useListEnvironments,
  useListToolsets,
} from "@gram/client/react-query/index.js";
import { Icon, ResizablePanel, Stack } from "@speakeasy-api/moonshine";
import { useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { v7 as uuidv7 } from "uuid";
import { OnboardingContent } from "../onboarding/Onboarding";
import { ToolsetView } from "../toolsets/Toolset";
import { ToolsetDropdown } from "../toolsets/ToolsetDropown";
import { AgentifyProvider } from "./Agentify";
import { ChatProvider, useChatContext } from "./ChatContext";
import { ChatConfig } from "./ChatWindow";
import { PlaygroundRHS } from "./PlaygroundRHS";

export default function Playground() {
  return (
    <ChatProvider>
      <AgentifyProvider>
        <PlaygroundInner />
      </AgentifyProvider>
    </ChatProvider>
  );
}

function PlaygroundInner() {
  const [searchParams] = useSearchParams();
  const { data: chatsData, refetch: refetchChats } = useListChats();
  const chat = useChatContext();
  const telemetry = useTelemetry();

  const [selectedToolset, setSelectedToolset] = useState<string | null>(
    searchParams.get("toolset") ?? null
  );
  const [selectedEnvironment, setSelectedEnvironment] = useState<string | null>(
    searchParams.get("environment") ?? null
  );
  const [dynamicToolset, setDynamicToolset] = useState(false);

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

  useRegisterToolsetTelemetry({
    toolsetSlug: selectedToolset ?? "",
  });
  useRegisterEnvironmentTelemetry({
    environmentSlug: selectedEnvironment ?? "",
  });

  const chatHistoryItems: DropdownItem[] =
    chatsData?.chats
      .sort((a, b) => b.updatedAt.getTime() - a.updatedAt.getTime())
      .map((chat) => ({
        label: capitalize(dateTimeFormatters.humanize(chat.updatedAt)),
        value: chat.id,
      })) ?? [];

  chatHistoryItems.unshift({
    icon: <Icon name="plus" />,
    label: "New chat",
    value: uuidv7(),
  });

  const chatHistoryButton = (
    <Combobox
      items={chatHistoryItems}
      onSelectionChange={(item) => {
        chat.setId(item.value);
      }}
      selected={chat.id}
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
        <Type variant="small" className="font-medium">
          Chat History
        </Type>
      </Stack>
    </Combobox>
  );

  const shareChatButton = (
    <Button
      size="sm"
      icon="link"
      variant={"ghost"}
      onClick={() => {
        telemetry.capture("chat_event", {
          action: "chat_shared",
        });
        navigator.clipboard.writeText(chat.url);
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
      <Page.Body className="max-w-full p-0">
        <ResizablePanel
          direction="horizontal"
          className="h-full [&>[role='separator']]:border-border [&>[role='separator']]:border-1"
        >
          <ResizablePanel.Pane minSize={35}>
            <ToolsetPanel
              configRef={chatConfigRef}
              setSelectedToolset={setSelectedToolset}
              setSelectedEnvironment={setSelectedEnvironment}
              dynamicToolset={dynamicToolset}
              setDynamicToolset={setDynamicToolset}
            />
          </ResizablePanel.Pane>
          <ResizablePanel.Pane minSize={35} order={0}>
            <PlaygroundRHS
              configRef={chatConfigRef}
              dynamicToolset={dynamicToolset}
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
    <div className="max-h-full overflow-scroll relative">
      <PanelHeader side="left">
        <Stack direction="horizontal" gap={2} justify="space-between">
          <Stack direction="horizontal" gap={2} align="center">
            <ToolsetDropdown
              selectedToolset={toolset}
              setSelectedToolset={(toolset) => setSelectedToolset(toolset.slug)}
            />
            {isAdmin && (
              <Stack direction="horizontal" align="center">
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
              </Stack>
            )}
          </Stack>
          {environmentDropdownItems.length > 1 && (
            <Stack direction="horizontal" gap={2} align="center">
              <Heading variant="h5">Active environment: </Heading>
              {environmentDropdown}
            </Stack>
          )}
        </Stack>
      </PanelHeader>
      <ToolsetView
        toolsetSlug={selectedToolset ?? ""}
        className="p-8 2xl:p-12"
        environmentSlug={selectedEnvironment ?? undefined}
      />
    </div>
  );
}

export const PanelHeader = ({
  side,
  children,
}: {
  side: "left" | "right";
  children: React.ReactNode;
}) => {
  return (
    <div
      className={cn(
        "sticky top-0 bg-stone-100 dark:bg-card py-3 px-8 border-b z-10 h-[61px]",
        side === "left" && "dark:border-l-2 dark:border-l-background",
        side === "right" && "dark:border-r-2 dark:border-r-background"
      )}
    >
      {children}
    </div>
  );
};
