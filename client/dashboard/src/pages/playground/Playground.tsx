import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { Combobox, DropdownItem } from "@/components/ui/combobox";
import { Type } from "@/components/ui/type";
import {
  useRegisterEnvironmentTelemetry,
  useRegisterToolsetTelemetry,
  useTelemetry,
} from "@/contexts/Telemetry";
import { dateTimeFormatters } from "@/lib/dates";
import { capitalize } from "@/lib/utils";
import { useRoutes } from "@/routes";
import {
  useListChats,
  useListToolsets,
  useListEnvironments,
  useInstance,
  useUpdateToolsetMutation,
  queryKeyInstance,
  queryKeyListToolsets,
} from "@gram/client/react-query/index.js";
import { useQueryClient } from "@tanstack/react-query";
import { Icon, ResizablePanel, Stack } from "@speakeasy-api/moonshine";
import { useEffect, useMemo, useRef, useState } from "react";
import { useLatestDeployment } from "@/hooks/toolTypes";
import { useSearchParams } from "react-router";
import { toast } from "sonner";
import { v7 as uuidv7 } from "uuid";
import { asTool, Tool } from "@/lib/toolTypes";
import { ToolsetsEmptyState } from "../toolsets/ToolsetsEmptyState";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ChatProvider, useChatContext } from "./ChatContext";
import { ManageToolsDialog } from "./ManageToolsDialog";
import { EditToolDialog } from "./EditToolDialog";
import { PlaygroundAuth, getAuthStatus } from "./PlaygroundAuth";
import { ChatConfig } from "./ChatWindow";
import { PlaygroundRHS } from "./PlaygroundRHS";
import { PlaygroundConfigPanel } from "./PlaygroundConfigPanel";
import { PlaygroundLogsPanel } from "./PlaygroundLogsPanel";
import { ScrollTextIcon } from "lucide-react";
import { useEnvironment } from "../environments/Environment";

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
    searchParams.get("toolset") ?? null,
  );
  const [selectedEnvironment, setSelectedEnvironment] = useState<string | null>(
    searchParams.get("environment") ?? null,
  );
  const [showLogs, setShowLogs] = useState(false);
  const [temperature, setTemperature] = useState(0.5);
  const [model, setModel] = useState("anthropic/claude-sonnet-4.5");
  const [maxTokens, setMaxTokens] = useState(4096);

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

  // Fetch toolsets and instance data
  const { data: toolsetsData } = useListToolsets();
  const toolsets = toolsetsData?.toolsets;
  const toolset = toolsets?.find((ts) => ts.slug === selectedToolset);

  const environmentData = useEnvironment(selectedEnvironment ?? undefined);

  // Check auth status
  const authStatus = useMemo(() => {
    if (!toolset || !environmentData) {
      return null;
    }
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    return getAuthStatus(toolset as any, {
      entries: environmentData.entries?.map((e) => ({
        name: e.name,
        value: e.value,
      })),
    });
  }, [toolset, environmentData]);

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

  const logsButton = (
    <Button size="sm" variant="ghost" onClick={() => setShowLogs(!showLogs)}>
      <ScrollTextIcon className="size-4 mr-2" />
      {showLogs ? "Hide" : "Show"} Logs
    </Button>
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs fullWidth />
      </Page.Header>
      <Page.Body fullWidth fullHeight className="p-0">
        <ResizablePanel
          direction="horizontal"
          className="h-full [&>[role='separator']]:w-px [&>[role='separator']]:bg-neutral-softest [&>[role='separator']]:border-0 [&>[role='separator']]:hover:bg-primary [&>[role='separator']]:relative [&>[role='separator']]:before:absolute [&>[role='separator']]:before:inset-y-0 [&>[role='separator']]:before:-left-1 [&>[role='separator']]:before:-right-1 [&>[role='separator']]:before:cursor-col-resize"
        >
          <ResizablePanel.Pane minSize={20} defaultSize={25}>
            <ToolsetPanel
              configRef={chatConfigRef}
              setSelectedToolset={setSelectedToolset}
              setSelectedEnvironment={setSelectedEnvironment}
              temperature={temperature}
              setTemperature={setTemperature}
              model={model}
              setModel={setModel}
              maxTokens={maxTokens}
              setMaxTokens={setMaxTokens}
            />
          </ResizablePanel.Pane>
          <ResizablePanel.Pane minSize={35} order={0}>
            <div className="h-full flex flex-col">
              {/* Action buttons below header */}
              <div className="flex items-center justify-between px-8 py-3">
                <div className="flex items-center gap-2">
                  {chatHistoryButton}
                </div>
                <div className="flex items-center gap-2">
                  {logsButton}
                  {shareChatButton}
                </div>
              </div>
              <div className="flex-1 overflow-hidden">
                <PlaygroundRHS
                  configRef={chatConfigRef}
                  initialPrompt={initialPrompt}
                  temperature={temperature}
                  model={model}
                  maxTokens={maxTokens}
                  authWarning={
                    authStatus?.hasMissingAuth && toolset
                      ? {
                          missingCount: authStatus.missingCount,
                          toolsetSlug: toolset.slug,
                        }
                      : null
                  }
                />
              </div>
            </div>
          </ResizablePanel.Pane>
          {showLogs && (
            <ResizablePanel.Pane minSize={20} defaultSize={30}>
              <PlaygroundLogsPanel
                chatId={chat.id}
                toolsetSlug={selectedToolset ?? undefined}
                onClose={() => setShowLogs(false)}
              />
            </ResizablePanel.Pane>
          )}
        </ResizablePanel>
      </Page.Body>
    </Page>
  );
}

export function ToolsetPanel({
  configRef,
  setSelectedToolset,
  setSelectedEnvironment,
  temperature,
  setTemperature,
  model,
  setModel,
  maxTokens,
  setMaxTokens,
}: {
  configRef: ChatConfig;
  setSelectedToolset: (toolset: string) => void;
  setSelectedEnvironment: (environment: string) => void;
  temperature: number;
  setTemperature: (temp: number) => void;
  model: string;
  setModel: (model: string) => void;
  maxTokens: number;
  setMaxTokens: (tokens: number) => void;
}) {
  const [showManageToolsDialog, setShowManageToolsDialog] = useState(false);
  const [manageToolsGroup, setManageToolsGroup] = useState<
    string | undefined
  >();
  const [editingTool, setEditingTool] = useState<Tool | null>(null);

  const { data: toolsetsData } = useListToolsets();
  const { data: environmentsData } = useListEnvironments();
  const routes = useRoutes();
  const updateToolsetMutation = useUpdateToolsetMutation();
  const queryClient = useQueryClient();

  const toolsets = toolsetsData?.toolsets;
  const environments = environmentsData?.environments;

  const selectedToolset = configRef.current.toolsetSlug;
  const selectedEnvironment = configRef.current.environmentSlug;

  const toolset = toolsets?.find((toolset) => toolset.slug === selectedToolset);

  const environmentData = useEnvironment(selectedEnvironment ?? undefined);

  // Fetch instance data to get tools
  const { data: instanceData } = useInstance(
    {
      toolsetSlug: selectedToolset ?? "",
    },
    undefined,
    {
      enabled: !!selectedToolset && !!selectedEnvironment,
    },
  );

  const { data: deployment } = useLatestDeployment();

  const documentIdToName = useMemo(() => {
    return deployment?.deployment?.openapiv3Assets?.reduce(
      (acc, asset) => {
        acc[asset.id] = asset.name;
        return acc;
      },
      {} as Record<string, string>,
    );
  }, [deployment]);

  const functionIdToName = useMemo(() => {
    return deployment?.deployment?.functionsAssets?.reduce(
      (acc, asset) => {
        acc[asset.id] = asset.name;
        return acc;
      },
      {} as Record<string, string>,
    );
  }, [deployment]);

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

  // Transform tools data for the config panel
  const tools = useMemo(
    () => instanceData?.tools?.map(asTool) ?? [],
    [instanceData?.tools],
  );

  // Track which tools are selected for bulk actions
  const [enabledTools, setEnabledTools] = useState<Set<string>>(new Set());

  // Handler for adding tools to the toolset
  const handleAddTools = (toolUrns: string[]) => {
    if (!toolset) return;
    const currentUrns = toolset.toolUrns || [];
    const updatedUrns = [...currentUrns, ...toolUrns];

    updateToolsetMutation.mutate(
      {
        request: {
          slug: toolset.slug,
          updateToolsetRequestBody: {
            toolUrns: updatedUrns,
          },
        },
      },
      {
        onSuccess: () => {
          // Invalidate both toolsets and instance queries to refresh the UI
          queryClient.invalidateQueries({
            queryKey: queryKeyListToolsets({}),
          });
          if (selectedToolset && selectedEnvironment) {
            queryClient.invalidateQueries({
              queryKey: queryKeyInstance({
                toolsetSlug: selectedToolset,
              }),
            });
          }
          toast.success(
            `Added ${toolUrns.length} tool${toolUrns.length !== 1 ? "s" : ""}`,
          );
        },
        onError: () => {
          toast.error("Failed to add tools");
        },
      },
    );
  };

  // Handler for removing tools from the toolset
  const handleRemoveTools = (toolUrns: string[]) => {
    if (!toolset) return;
    const currentUrns = toolset.toolUrns || [];
    const updatedUrns = currentUrns.filter((urn) => !toolUrns.includes(urn));

    updateToolsetMutation.mutate(
      {
        request: {
          slug: toolset.slug,
          updateToolsetRequestBody: {
            toolUrns: updatedUrns,
          },
        },
      },
      {
        onSuccess: () => {
          // Invalidate both toolsets and instance queries to refresh the UI
          queryClient.invalidateQueries({
            queryKey: queryKeyListToolsets({}),
          });
          if (selectedToolset && selectedEnvironment) {
            queryClient.invalidateQueries({
              queryKey: queryKeyInstance({
                toolsetSlug: selectedToolset,
              }),
            });
          }
          toast.success(
            `Removed ${toolUrns.length} tool${toolUrns.length !== 1 ? "s" : ""}`,
          );
        },
        onError: () => {
          toast.error("Failed to remove tools");
        },
      },
    );
  };

  // If listToolsets has completed and there's nothing there, show the onboarding panel
  if (toolsets !== undefined && !configRef.current.toolsetSlug) {
    return (
      <div className="h-full flex items-center justify-center p-8">
        <ToolsetsEmptyState onCreateToolset={() => routes.toolsets.goTo()} />
      </div>
    );
  }

  return (
    <>
      <PlaygroundConfigPanel
        tools={tools}
        selectedTools={enabledTools}
        onToolToggle={(toolId) => {
          setEnabledTools((prev) => {
            const next = new Set(prev);
            if (next.has(toolId)) {
              next.delete(toolId);
            } else {
              next.add(toolId);
            }
            return next;
          });
        }}
        temperature={temperature}
        onTemperatureChange={setTemperature}
        model={model}
        onModelChange={setModel}
        maxTokens={maxTokens}
        onMaxTokensChange={setMaxTokens}
        toolsetSelector={
          <Select
            value={selectedToolset ?? undefined}
            onValueChange={setSelectedToolset}
          >
            <SelectTrigger size="sm" className="w-full">
              <SelectValue placeholder="Select toolset" />
            </SelectTrigger>
            <SelectContent>
              {toolsets?.map((ts) => (
                <SelectItem key={ts.slug} value={ts.slug}>
                  {ts.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        }
        environmentSelector={
          <Select
            value={selectedEnvironment ?? undefined}
            onValueChange={setSelectedEnvironment}
          >
            <SelectTrigger size="sm" className="w-full">
              <SelectValue placeholder="Select environment" />
            </SelectTrigger>
            <SelectContent>
              {environments?.map((env) => (
                <SelectItem key={env.slug} value={env.slug}>
                  {env.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        }
        authSettings={
          toolset && environmentData ? (
            <PlaygroundAuth
              // eslint-disable-next-line @typescript-eslint/no-explicit-any
              toolset={toolset as any}
              environment={{
                slug: environmentData.slug,
                entries: environmentData.entries?.map((e) => ({
                  name: e.name,
                  value: e.value,
                })),
              }}
            />
          ) : undefined
        }
        toolsetInfo={
          toolset
            ? {
                name: toolset.name,
                slug: toolset.slug,
                description: toolset.description,
                toolCount: tools.length,
                updatedAt: toolset.updatedAt,
              }
            : undefined
        }
        onToolsetUpdate={(updates) => {
          // TODO: Wire this up to update toolset
          console.log("Update toolset:", updates);
        }}
        documentIdToName={documentIdToName}
        functionIdToName={functionIdToName}
        onOpenToolsModal={() => {
          setManageToolsGroup(undefined);
          setShowManageToolsDialog(true);
        }}
        onOpenGroupModal={(groupTitle) => {
          setManageToolsGroup(groupTitle);
          setShowManageToolsDialog(true);
        }}
        onToolClick={(tool) => {
          setEditingTool(tool);
        }}
      />

      {/* ManageToolsDialog */}
      {toolset && (
        <ManageToolsDialog
          open={showManageToolsDialog}
          onOpenChange={setShowManageToolsDialog}
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          toolset={toolset as any}
          currentTools={tools}
          onAddTools={(toolUrns) => handleAddTools(toolUrns)}
          onRemoveTools={(toolUrns) => handleRemoveTools(toolUrns)}
          initialGroup={manageToolsGroup}
        />
      )}

      {/* EditToolDialog */}
      <EditToolDialog
        open={!!editingTool}
        onOpenChange={(open) => !open && setEditingTool(null)}
        tool={editingTool}
        documentIdToName={documentIdToName}
        functionIdToName={functionIdToName}
        onSave={() => {
          // TODO: Implement tool variation updates
          toast.success("Tool updated");
          setEditingTool(null);
        }}
        onRemove={() => {
          if (editingTool?.toolUrn) {
            handleRemoveTools([editingTool.toolUrn]);
          }
          setEditingTool(null);
        }}
      />
    </>
  );
}
