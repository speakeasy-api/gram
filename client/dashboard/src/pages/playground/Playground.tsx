import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  useRegisterEnvironmentTelemetry,
  useRegisterToolsetTelemetry,
} from "@/contexts/Telemetry";
import { useLatestDeployment } from "@/hooks/toolTypes";
import { asTools, Tool } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import {
  queryKeyInstance,
  queryKeyListToolsets,
  useInstance,
  useListEnvironments,
  useListToolsets,
  useUpdateToolsetMutation,
} from "@gram/client/react-query/index.js";
import { ResizablePanel } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { ScrollTextIcon } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { toast } from "@/lib/toast";
import { useEnvironment } from "../environments/Environment";
import { ToolsetsEmptyState } from "../toolsets/ToolsetsEmptyState";
import { ChatProvider, useChatContext } from "./ChatContext";
import { ChatConfig } from "./ChatWindow";
import { EditToolDialog } from "./EditToolDialog";
import { ManageToolsDialog } from "./ManageToolsDialog";
import { PlaygroundAuth } from "./PlaygroundAuth";
import { PlaygroundConfigPanel } from "./PlaygroundConfigPanel";
import { PlaygroundElements } from "./PlaygroundElements";
import { PlaygroundLogsPanel } from "./PlaygroundLogsPanel";

export default function Playground() {
  return (
    <ChatProvider>
      <PlaygroundInner />
    </ChatProvider>
  );
}

function PlaygroundInner() {
  const [searchParams] = useSearchParams();
  const chat = useChatContext();

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
              <PlaygroundElements
                toolsetSlug={selectedToolset}
                environmentSlug={selectedEnvironment}
                model={model}
                additionalActions={
                  <div className="flex items-center justify-end w-full px-4">
                    {logsButton}
                  </div>
                }
              />
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
    () => asTools(instanceData?.tools ?? []),
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
          if (selectedToolset) {
            // Use partial query key (toolsetSlug only) to match all instances
            // of this toolset, regardless of environment
            queryClient.invalidateQueries({
              queryKey: queryKeyInstance({
                toolsetSlug: selectedToolset,
              }),
            });
          }
          toast.success(
            `Added ${toolUrns.length} tool${toolUrns.length !== 1 ? "s" : ""}`,
            { persist: true },
          );
        },
        onError: () => {
          toast.error("Failed to add tools", { persist: true });
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
          if (selectedToolset) {
            // Use partial query key (toolsetSlug only) to match all instances
            // of this toolset, regardless of environment
            queryClient.invalidateQueries({
              queryKey: queryKeyInstance({
                toolsetSlug: selectedToolset,
              }),
            });
          }
          toast.success(
            `Removed ${toolUrns.length} tool${toolUrns.length !== 1 ? "s" : ""}`,
            { persist: true },
          );
        },
        onError: () => {
          toast.error("Failed to remove tools", { persist: true });
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
          toast.success("Tool updated", { persist: true });
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
