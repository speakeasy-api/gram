import { Page } from "@/components/page-layout";
import { WorkbenchLayout } from "@/components/layouts/workbench-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/button";
import { ResizablePanel } from "@/components/ui/resizable-panel";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import {
  useRegisterEnvironmentTelemetry,
  useRegisterToolsetTelemetry,
} from "@/contexts/Telemetry";
import { useLatestDeployment, useToolset } from "@/hooks/toolTypes";
import { DEFAULT_MODEL } from "@/lib/models";
import { Tool } from "@/lib/toolTypes";
import { useRoutes } from "@/routes";
import { useHideInsightsDock } from "@/components/insights-context";
import { Confirm } from "@gram/client/models/components/upsertglobaltoolvariationform.js";
import { queryKeyInstance } from "@gram/client/react-query/instance.js";
import {
  queryKeyListToolsets,
  useListToolsets,
} from "@gram/client/react-query/listToolsets.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateTemplate } from "@gram/client/react-query/template.js";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { useUpdateToolsetMutation } from "@gram/client/react-query/updateToolset.js";
import { useQueryClient } from "@tanstack/react-query";
import { MessageCircle, Plus, ScrollTextIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { toast } from "sonner";
import { ChatProvider } from "./ChatContext";
import { useChatContext } from "./useChatContext";
import { EditToolDialog, ToolUpdatePayload } from "./EditToolDialog";
import { ManageToolsDialog } from "./ManageToolsDialog";
import { PlaygroundAuth } from "./PlaygroundAuth";
import { PlaygroundConfigPanel } from "./PlaygroundConfigPanel";
import { PlaygroundElements } from "./PlaygroundElements";
import { PlaygroundLogsPanel } from "./PlaygroundLogsPanel";
import { PlaygroundRemoteChat } from "./PlaygroundRemoteChat";
import { ShareChatButton } from "./ShareChatButton";
import { useRemoteMcpConnection } from "./useRemoteMcpConnection";

// A single selectable server in the playground. Toolset-backed and
// remote-MCP-backed servers share one flat picker; the `kind` discriminant only
// drives how we connect (and which controls appear), never how it's labeled.
type PlaygroundServerRef =
  | { kind: "toolset"; key: string; name: string; toolsetSlug: string }
  | {
      kind: "remote";
      key: string;
      name: string;
      mcpServerId: string;
      isIssuerGated: boolean;
    };

const toolsetServerKey = (slug: string) => `toolset:${slug}`;
const remoteServerKey = (mcpServerId: string) => `remote:${mcpServerId}`;

// Merges toolset-backed servers (from listToolsets) with remote-MCP-backed
// servers (the remoteMcpServerId subset of mcpServers) into one sorted list.
// Neither source overlaps the other, so nothing is double-counted.
function usePlaygroundServers(): {
  servers: PlaygroundServerRef[];
  isLoading: boolean;
} {
  const { data: toolsetsData, isLoading: isLoadingToolsets } =
    useListToolsets();
  const { data: mcpServersData, isLoading: isLoadingMcpServers } =
    useMcpServers();

  const servers = useMemo<PlaygroundServerRef[]>(() => {
    const toolsetServers: PlaygroundServerRef[] = (
      toolsetsData?.toolsets ?? []
    ).map((toolset) => ({
      kind: "toolset",
      key: toolsetServerKey(toolset.slug),
      name: toolset.name,
      toolsetSlug: toolset.slug,
    }));

    const remoteServers: PlaygroundServerRef[] = (
      mcpServersData?.mcpServers ?? []
    )
      .filter((server) => !!server.remoteMcpServerId)
      .map((server) => ({
        kind: "remote",
        key: remoteServerKey(server.id),
        name: server.name ?? server.slug ?? "Remote MCP server",
        mcpServerId: server.id,
        isIssuerGated: !!server.userSessionIssuerId,
      }));

    return [...toolsetServers, ...remoteServers].sort((a, b) =>
      a.name.localeCompare(b.name),
    );
  }, [toolsetsData, mcpServersData]);

  return { servers, isLoading: isLoadingToolsets || isLoadingMcpServers };
}

function PlaygroundEmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <MessageCircle className="text-muted-foreground h-6 w-6" />
      </div>
      <Type variant="subheading" className="mb-1">
        No MCP servers yet
      </Type>
      <Type small muted className="mb-4 max-w-md text-center">
        The playground lets you chat with tools from an MCP server. Create one
        to start testing.
      </Type>
      <RequireScope scope="mcp:write" level="component">
        {({ disabled }) => (
          <Button onClick={onCreate} disabled={disabled}>
            <Plus className="mr-2 h-4 w-4" />
            Create MCP Server
          </Button>
        )}
      </RequireScope>
    </div>
  );
}

export default function Playground(): JSX.Element {
  // The playground hosts its own chat runtime, so hide the floating dock (and
  // keep the shared runtime out of this page's tree — two RemoteThreadListRuntimes
  // cannot nest).
  useHideInsightsDock();
  return (
    <RequireScope scope={["mcp:read", "mcp:write", "mcp:connect"]} level="page">
      <ChatProvider>
        <PlaygroundInner />
      </ChatProvider>
    </RequireScope>
  );
}

/** Resolve the initially-selected server key from URL params. */
function initialServerKey(searchParams: URLSearchParams): string | null {
  const mcpServer = searchParams.get("mcpServer");
  if (mcpServer) return remoteServerKey(mcpServer);
  const toolset = searchParams.get("toolset");
  if (toolset) return toolsetServerKey(toolset);
  return null;
}

function PlaygroundInner() {
  const [searchParams] = useSearchParams();
  const chat = useChatContext();
  const routes = useRoutes();

  const { servers, isLoading: isLoadingServers } = usePlaygroundServers();

  const [selectedKey, setSelectedKey] = useState<string | null>(() =>
    initialServerKey(searchParams),
  );
  const [selectedEnvironment, setSelectedEnvironment] = useState<string | null>(
    searchParams.get("environment") ?? null,
  );
  const [showLogs, setShowLogs] = useState(false);
  const [temperature, setTemperature] = useState(0.5);
  const [model, setModel] = useState<string>(DEFAULT_MODEL);
  const [maxTokens, setMaxTokens] = useState(4096);
  const [playgroundEnvironmentSlug, setPlaygroundEnvironmentSlug] = useState<
    string | undefined
  >(undefined);

  const selectedServer = useMemo(
    () => servers.find((s) => s.key === selectedKey) ?? null,
    [servers, selectedKey],
  );

  // Auto-select the first server once the list loads and nothing is chosen.
  useEffect(() => {
    if (!selectedKey && servers[0]) {
      setSelectedKey(servers[0].key);
    }
  }, [servers, selectedKey]);

  const selectedToolsetSlug =
    selectedServer?.kind === "toolset" ? selectedServer.toolsetSlug : "";

  useRegisterToolsetTelemetry({ toolsetSlug: selectedToolsetSlug });
  useRegisterEnvironmentTelemetry({
    environmentSlug: selectedEnvironment ?? "",
  });

  const handleSelectServer = (key: string) => {
    setSelectedKey(key);
    // Reset the environment; the toolset panel re-defaults it for toolset servers.
    setSelectedEnvironment(null);
    setPlaygroundEnvironmentSlug(undefined);
  };

  const serverSelector = (
    <Select value={selectedKey ?? undefined} onValueChange={handleSelectServer}>
      <SelectTrigger size="sm" className="w-full">
        <SelectValue placeholder="Select MCP" />
      </SelectTrigger>
      <SelectContent>
        {servers.map((server) => (
          <SelectItem key={server.key} value={server.key}>
            {server.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );

  // Toolsets have loaded and there are none: full-page empty state.
  if (!isLoadingServers && servers.length === 0) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs fullWidth />
        </Page.Header>
        <Page.Body>
          <Page.Section>
            <Page.Section.Title>Playground</Page.Section.Title>
            <Page.Section.Description className="max-w-2xl">
              Test your MCP servers and tools with a chat interface.
            </Page.Section.Description>
            <Page.Section.Body>
              <PlaygroundEmptyState onCreate={() => routes.mcp.goTo()} />
            </Page.Section.Body>
          </Page.Section>
        </Page.Body>
      </Page>
    );
  }

  const logsButton = (
    <Button size="sm" variant="tertiary" onClick={() => setShowLogs(!showLogs)}>
      <ScrollTextIcon className="mr-2 size-4" />
      {showLogs ? "Hide" : "Show"} Logs
    </Button>
  );

  const additionalActions = (
    <div className="flex w-full items-center justify-end px-4">
      <ShareChatButton />
      {logsButton}
    </div>
  );

  const configPane = (
    <>
      {selectedServer?.kind === "toolset" && (
        <ToolsetPanel
          toolsetSlug={selectedServer.toolsetSlug}
          serverSelector={serverSelector}
          setSelectedEnvironment={setSelectedEnvironment}
          temperature={temperature}
          setTemperature={setTemperature}
          model={model}
          setModel={setModel}
          maxTokens={maxTokens}
          setMaxTokens={setMaxTokens}
          onPlaygroundEnvironmentSlug={setPlaygroundEnvironmentSlug}
        />
      )}
      {selectedServer?.kind === "remote" && (
        <RemoteServerPanel
          mcpServerId={selectedServer.mcpServerId}
          isIssuerGated={selectedServer.isIssuerGated}
          serverSelector={serverSelector}
          temperature={temperature}
          setTemperature={setTemperature}
          model={model}
          setModel={setModel}
          maxTokens={maxTokens}
          setMaxTokens={setMaxTokens}
        />
      )}
    </>
  );

  const chatArea = (
    <div className="flex h-full flex-col">
      {!selectedServer && (
        <div className="flex h-full items-center justify-center">
          <Type muted>Select an MCP server to start chatting</Type>
        </div>
      )}
      {selectedServer?.kind === "toolset" && (
        <PlaygroundElements
          toolsetSlug={selectedServer.toolsetSlug}
          environmentSlug={selectedEnvironment}
          model={model}
          playgroundEnvironmentSlug={playgroundEnvironmentSlug}
          additionalActions={additionalActions}
        />
      )}
      {selectedServer?.kind === "remote" && (
        <PlaygroundRemoteChat
          mcpServerId={selectedServer.mcpServerId}
          isIssuerGated={selectedServer.isIssuerGated}
          environmentSlug={selectedEnvironment}
          model={model}
          additionalActions={additionalActions}
        />
      )}
    </div>
  );

  const previewNode = showLogs ? (
    <ResizablePanel
      direction="horizontal"
      className="[&>[role='separator']]:bg-neutral-softest [&>[role='separator']]:hover:bg-primary h-full [&>[role='separator']]:relative [&>[role='separator']]:w-px [&>[role='separator']]:border-0 [&>[role='separator']]:before:absolute [&>[role='separator']]:before:inset-y-0 [&>[role='separator']]:before:-right-1 [&>[role='separator']]:before:-left-1 [&>[role='separator']]:before:cursor-col-resize"
    >
      <ResizablePanel.Pane minSize={35}>{chatArea}</ResizablePanel.Pane>
      <ResizablePanel.Pane minSize={20} defaultSize={30}>
        <PlaygroundLogsPanel
          chatId={chat.id}
          toolsetSlug={
            selectedServer?.kind === "toolset"
              ? selectedServer.toolsetSlug
              : undefined
          }
          onClose={() => setShowLogs(false)}
        />
      </ResizablePanel.Pane>
    </ResizablePanel>
  ) : (
    chatArea
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs fullWidth />
      </Page.Header>
      <Page.Body fullWidth fullHeight noPadding>
        <WorkbenchLayout>
          <WorkbenchLayout.Header eyebrow="Playground" title="Playground" />
          <WorkbenchLayout.Body config={configPane} preview={previewNode} />
        </WorkbenchLayout>
      </Page.Body>
    </Page>
  );
}

interface PanelConfigProps {
  serverSelector: React.ReactNode;
  temperature: number;
  setTemperature: (temp: number) => void;
  model: string;
  setModel: (model: string) => void;
  maxTokens: number;
  setMaxTokens: (tokens: number) => void;
}

function ToolsetPanel({
  toolsetSlug,
  serverSelector,
  setSelectedEnvironment,
  temperature,
  setTemperature,
  model,
  setModel,
  maxTokens,
  setMaxTokens,
  onPlaygroundEnvironmentSlug,
}: PanelConfigProps & {
  toolsetSlug: string;
  setSelectedEnvironment: (environment: string) => void;
  onPlaygroundEnvironmentSlug?: (slug: string | undefined) => void;
}) {
  const [showManageToolsDialog, setShowManageToolsDialog] = useState(false);
  const [manageToolsGroup, setManageToolsGroup] = useState<
    string | undefined
  >();
  const [editingTool, setEditingTool] = useState<Tool | null>(null);

  const client = useSdkClient();
  const updateToolsetMutation = useUpdateToolsetMutation();
  const queryClient = useQueryClient();

  const { data: toolset } = useToolset(toolsetSlug);
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
    if (toolset?.defaultEnvironmentSlug) {
      setSelectedEnvironment(toolset.defaultEnvironmentSlug);
    }
  }, [setSelectedEnvironment, toolset]);

  // Track which tools are selected for bulk actions
  const [enabledTools, setEnabledTools] = useState<Set<string>>(new Set());

  const invalidateToolset = () => {
    void queryClient.invalidateQueries({ queryKey: queryKeyListToolsets({}) });
    void queryClient.invalidateQueries({
      queryKey: queryKeyInstance({ toolsetSlug }),
    });
  };

  // Handler for adding tools to the toolset
  const handleAddTools = (toolUrns: string[]) => {
    if (!toolset) return;
    const updatedUrns = [...(toolset.toolUrns || []), ...toolUrns];

    updateToolsetMutation.mutate(
      {
        request: {
          slug: toolset.slug,
          updateToolsetRequestBody: { toolUrns: updatedUrns },
        },
      },
      {
        onSuccess: () => {
          invalidateToolset();
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
    const updatedUrns = (toolset.toolUrns || []).filter(
      (urn) => !toolUrns.includes(urn),
    );

    updateToolsetMutation.mutate(
      {
        request: {
          slug: toolset.slug,
          updateToolsetRequestBody: { toolUrns: updatedUrns },
        },
      },
      {
        onSuccess: () => {
          invalidateToolset();
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

  const handleToolUpdate = async (tool: Tool, updates: ToolUpdatePayload) => {
    if (tool.type === "prompt") {
      await client.templates.update({
        updatePromptTemplateForm: {
          ...tool,
          ...updates,
        },
      });
      void invalidateTemplate(queryClient, [{ name: tool.name }]);
    } else {
      const form = {
        ...tool.variation,
        confirm: tool.variation?.confirm as Confirm,
        ...updates,
        srcToolName: tool.canonicalName,
        srcToolUrn: tool.toolUrn,
      };
      await client.variations.upsertGlobal({
        upsertGlobalToolVariationForm: form,
      });
    }

    // Invalidate to refresh tool data in the sidebar
    void invalidateAllToolset(queryClient);
    void queryClient.invalidateQueries({
      queryKey: queryKeyInstance({ toolsetSlug }),
    });
  };

  return (
    <>
      <PlaygroundConfigPanel
        tools={toolset?.tools ?? []}
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
        toolsetSelector={serverSelector}
        authSettings={
          toolset ? (
            <PlaygroundAuth
              // Force remount on toolset change so user-provided values
              // and edited keys reset and don't leak across toolsets.
              key={toolset.slug}
              toolset={toolset}
              onPlaygroundEnvironmentSlug={onPlaygroundEnvironmentSlug}
            />
          ) : undefined
        }
        toolsetInfo={
          toolset
            ? {
                name: toolset.name,
                slug: toolset.slug,
                description: toolset.description,
                toolCount: toolset.tools.length,
                updatedAt: toolset.updatedAt,
              }
            : undefined
        }
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
          toolset={toolset}
          currentTools={toolset.tools}
          onAddTools={(toolUrns) => handleAddTools(toolUrns)}
          onRemoveTools={(toolUrns) => handleRemoveTools(toolUrns)}
          initialGroup={manageToolsGroup}
        />
      )}

      {/* EditToolDialog */}
      <EditToolDialog
        open={!!editingTool}
        onOpenChange={(open) => {
          void (!open && setEditingTool(null));
        }}
        tool={editingTool}
        documentIdToName={documentIdToName}
        functionIdToName={functionIdToName}
        onSave={async (updates) => {
          if (!editingTool) return;
          try {
            await handleToolUpdate(editingTool, updates);
            toast.success("Tool updated");
          } catch (err) {
            toast.error("Failed to update tool");
            throw err;
          }
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

/**
 * Left panel for a remote-MCP-backed server: the shared selector, a read-only
 * live tool list, and model settings. Tool curation, auth, and env config are
 * absent — those affordances don't apply to a proxied upstream.
 */
function RemoteServerPanel({
  mcpServerId,
  isIssuerGated,
  serverSelector,
  temperature,
  setTemperature,
  model,
  setModel,
  maxTokens,
  setMaxTokens,
}: PanelConfigProps & {
  mcpServerId: string;
  isIssuerGated: boolean;
}) {
  const { tools } = useRemoteMcpConnection(mcpServerId, isIssuerGated);

  const remoteTools = useMemo(
    () =>
      Object.entries(tools ?? {}).map(([name, tool]) => ({
        name,
        description: tool.description,
      })),
    [tools],
  );

  return (
    <PlaygroundConfigPanel
      remoteTools={remoteTools}
      temperature={temperature}
      onTemperatureChange={setTemperature}
      model={model}
      onModelChange={setModel}
      maxTokens={maxTokens}
      onMaxTokensChange={setMaxTokens}
      toolsetSelector={serverSelector}
    />
  );
}
