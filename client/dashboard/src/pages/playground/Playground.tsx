import { Page } from "@/components/page-layout";
import { Button as MoonshineButton, Icon } from "@speakeasy-api/moonshine";
import { Button } from "@/components/ui/button";
import { Combobox, DropdownItem } from "@/components/ui/combobox";
import { Type } from "@/components/ui/type";
import { useIsAdmin } from "@/contexts/Auth";
import {
  useRegisterEnvironmentTelemetry,
  useRegisterToolsetTelemetry,
  useTelemetry,
} from "@/contexts/Telemetry";
import { dateTimeFormatters } from "@/lib/dates";
import { capitalize, cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import {
  useListChats,
  useListToolsets,
} from "@gram/client/react-query/index.js";
import { ResizablePanel, Stack } from "@speakeasy-api/moonshine";
import { useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { toast } from "sonner";
import { v7 as uuidv7 } from "uuid";
import { ToolsetView } from "../toolsets/Toolset";
import { ToolsetDropdown } from "../toolsets/ToolsetDropown";
import { ToolsetsEmptyState } from "../toolsets/ToolsetsEmptyState";
import { ChatProvider, useChatContext } from "./ChatContext";
import { ChatConfig } from "./ChatWindow";
import { PlaygroundRHS } from "./PlaygroundRHS";

export default function Playground() {
  return (
    <ChatProvider>
      <PlaygroundInner />
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
  
  // Get prompt from URL params if available
  const initialPrompt = searchParams.get("prompt");

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
      variant="ghost"
      icon="link"
      onClick={() => {
        telemetry.capture("chat_event", {
          action: "chat_shared",
        });
        navigator.clipboard.writeText(chat.url);
        toast.success("Chat link copied to clipboard");
      }}
    >
      Share chat
    </Button>
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs fullWidth />
        <Stack direction="horizontal" gap={2} align="center">
          {shareChatButton}
          {chatHistoryButton}
        </Stack>
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
              setSelectedEnvironment={setSelectedEnvironment}
              initialPrompt={initialPrompt}
            />
          </ResizablePanel.Pane>
        </ResizablePanel>
      </Page.Body>
    </Page>
  );
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
  const isAdmin = useIsAdmin();
  const routes = useRoutes();

  const toolsets = toolsetsData?.toolsets;

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
    if (
      configRef.current.environmentSlug === null &&
      toolset?.defaultEnvironmentSlug
    ) {
      setSelectedEnvironment(toolset.defaultEnvironmentSlug);
    }
  }, [configRef, setSelectedEnvironment, toolset]);

  // Don't automatically set dynamic toolset. The generate object API is not working consistently
  // useEffect(() => {
  //   const isDynamic =
  //     toolset?.httpTools?.length && toolset.httpTools.length > 40 && isAdmin;
  //   setDynamicToolset(!!isDynamic);
  // }, [toolset, isAdmin, setDynamicToolset]);

  let content = (
    <ToolsetView
      toolsetSlug={selectedToolset ?? ""}
      className="p-8 2xl:p-12"
      environmentSlug={selectedEnvironment ?? undefined}
      addToolsStyle={"modal"}
      showEnvironmentBadge
      noGrid
      onEnvironmentChange={setSelectedEnvironment}
      context="playground"
    />
  );

  // If listToolsets has completed and there's nothing there, show the onboarding panel
  if (toolsets !== undefined && !configRef.current.toolsetSlug) {
    // This should only be reachable if the user has an OpenAPI document but no toolsets
    content = (
      <div className="h-[600px] p-8">
        <ToolsetsEmptyState onCreateToolset={() => routes.toolsets.goTo()} />
      </div>
    );
  }

  return (
    <div className="max-h-full overflow-auto relative">
      <PanelHeader side="left">
        <Stack direction="horizontal" gap={2} justify="space-between">
          <Stack direction="horizontal" gap={2} align="center">
            <ToolsetDropdown
              selectedToolset={toolset}
              setSelectedToolset={(toolset) => setSelectedToolset(toolset.slug)}
            />
            {isAdmin && (
              <Stack direction="horizontal" align="center">
                <MoonshineButton
                  variant="tertiary"
                  onClick={() => setDynamicToolset(!dynamicToolset)}
                >
                  <MoonshineButton.LeftIcon>
                    <Icon name={dynamicToolset ? "sparkles" : "lock"} className="h-4 w-4" />
                  </MoonshineButton.LeftIcon>
                  <MoonshineButton.Text>{dynamicToolset ? "Dynamic" : "Static"}</MoonshineButton.Text>
                </MoonshineButton>
              </Stack>
            )}
          </Stack>
        </Stack>
      </PanelHeader>
      {content}
    </div>
  );
}

export const PanelHeader = ({
  side,
  children,
}: {
  side: "left" | "right";
  children?: React.ReactNode;
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
