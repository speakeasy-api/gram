import { ToolsetEnvironmentForm } from "@/components/environments/ToolsetEnvironmentForm";
import { InputDialog } from "@/components/input-dialog";
import { Page } from "@/components/page-layout";
import { ToolList } from "@/components/tool-list";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { MoreActions } from "@/components/ui/more-actions";
import { MultiSelect } from "@/components/ui/multi-select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useCommandPalette } from "@/contexts/CommandPalette";
import { useSdkClient } from "@/contexts/Sdk";
import {
  useRegisterToolsetTelemetry,
  useTelemetry,
} from "@/contexts/Telemetry";
import { useToolset } from "@/hooks/toolTypes";
import { handleAPIError } from "@/lib/errors";
import { Tool, useGroupedTools } from "@/lib/toolTypes";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Confirm } from "@gram/client/models/components";
import { invalidateTemplate } from "@gram/client/react-query";
import {
  queryKeyInstance,
  useCloneToolsetMutation,
  useCreateToolsetMutation,
  useDeleteToolsetMutation,
  useUpdateToolsetMutation,
} from "@gram/client/react-query/index.js";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useState } from "react";
import { Outlet, useLocation, useNavigate, useParams } from "react-router";
import { toast } from "sonner";
import { MCPDetails, MCPEnableButton } from "../mcp/MCPDetails";
import { AddToolsDialog } from "./AddToolsDialog";
import { PromptsTabContent } from "./PromptsTab";
import { ResourcesTabContent } from "./resources/ResourcesTab";
import { ServerTabContent } from "./ServerTab";
import { ToolsetAuthAlert } from "./ToolsetAuthAlert";
import { ToolsetEmptyState } from "./ToolsetEmptyState";
import { ToolsetHeader } from "./ToolsetHeader";
import { useToolsets } from "./Toolsets";

export function useDeleteToolset({
  onSuccess,
}: { onSuccess?: () => void } = {}) {
  const toolsets = useToolsets();
  const telemetry = useTelemetry();

  const mutation = useDeleteToolsetMutation({
    onSuccess: async () => {
      telemetry.capture("toolset_event", {
        action: "toolset_deleted",
      });
      await toolsets.refetch();
      onSuccess?.();
    },
    onError: (error) => {
      handleAPIError(error, "Failed to delete toolset");
    },
  });

  return (slug: string) => {
    if (
      confirm(
        "Are you sure you want to delete this toolset? This action cannot be undone.",
      )
    ) {
      mutation.mutate({
        request: {
          slug,
        },
      });
    }
  };
}

export function useCloneToolset({
  onSuccess,
}: { onSuccess?: () => void } = {}) {
  const toolsets = useToolsets();
  const telemetry = useTelemetry();
  const routes = useRoutes();
  const mutation = useCloneToolsetMutation({
    onSuccess: async (data) => {
      telemetry.capture("toolset_event", {
        action: "toolset_cloned",
        toolset_slug: data.slug,
      });
      toast.success(`Toolset cloned successfully as "${data.name}"`);
      await toolsets.refetch();
      routes.toolsets.toolset.goTo(data.slug);
      onSuccess?.();
    },
    onError: (error) => {
      handleAPIError(error, "Failed to clone toolset");
    },
  });

  return (slug: string) => {
    mutation.mutate({
      request: {
        slug,
      },
    });
  };
}

function AddToToolsetDialog({
  open,
  onOpenChange,
  toolUrns,
  currentToolsetSlug,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  toolUrns: string[];
  currentToolsetSlug: string;
}) {
  const toolsets = useToolsets();
  const telemetry = useTelemetry();
  const [selectedToolsetSlug, setSelectedToolsetSlug] = useState<string>("");

  const availableToolsets = toolsets.filter(
    (t) => t.slug !== currentToolsetSlug,
  );

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      telemetry.capture("toolset_event", {
        action: "tools_added_to_toolset",
        target_toolset: selectedToolsetSlug,
        tool_count: toolUrns.length,
      });
      toast.success(`Added ${toolUrns.length} tool(s) to toolset`);
      onOpenChange(false);
      setSelectedToolsetSlug("");
    },
  });

  const handleSubmit = async () => {
    const targetToolset = toolsets.find((t) => t.slug === selectedToolsetSlug);
    if (!targetToolset) return;

    const existingUrns = targetToolset.toolUrns || [];
    const newUrns = [...new Set([...existingUrns, ...toolUrns])];

    updateToolsetMutation.mutate({
      request: {
        slug: selectedToolsetSlug,
        updateToolsetRequestBody: {
          toolUrns: newUrns,
        },
      },
    });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Add to Toolset</Dialog.Title>
          <Dialog.Description>
            Add {toolUrns.length} selected tool(s) to another toolset
          </Dialog.Description>
        </Dialog.Header>
        <div className="flex flex-col gap-4 py-4">
          <div className="flex flex-col gap-2">
            <label className="text-sm font-medium">Select Toolset</label>
            <select
              className="w-full px-3 py-2 border border-neutral-200 rounded-md"
              value={selectedToolsetSlug}
              onChange={(e) => setSelectedToolsetSlug(e.target.value)}
            >
              <option value="">Choose a toolset...</option>
              {availableToolsets.map((toolset) => (
                <option key={toolset.slug} value={toolset.slug}>
                  {toolset.name}
                </option>
              ))}
            </select>
          </div>
        </div>
        <Dialog.Footer>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={!selectedToolsetSlug}>
            Add Tools
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

export function ToolsetRoot() {
  const { toolsetSlug } = useParams();
  const { data: toolset } = useToolset(toolsetSlug ?? "");

  useRegisterToolsetTelemetry({
    toolsetSlug: toolsetSlug ?? "",
  });

  if (!toolsetSlug) {
    return <div>Toolset not found</div>;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [toolsetSlug]: toolset?.name }}
        />
      </Page.Header>
      <Page.Body>
        <Outlet />
      </Page.Body>
    </Page>
  );
}

export default function ToolsetPage() {
  const { toolsetSlug } = useParams();
  if (!toolsetSlug) {
    return <div>Toolset not found</div>;
  }

  return <ToolsetView toolsetSlug={toolsetSlug} />;
}

type ToolsetTabs =
  | "tools"
  | "prompts"
  | "resources"
  | "auth"
  | "mcp"
  | "server";

export function ToolsetView({
  toolsetSlug,
  className,
  noGrid: _noGrid,
  context = "toolset",
}: {
  toolsetSlug: string;
  className?: string;
  addToolsStyle?: "link" | "modal";
  showEnvironmentBadge?: boolean;
  noGrid?: boolean;
  context?: "playground" | "toolset";
}) {
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const client = useSdkClient();
  const { data: toolset, refetch } = useToolset(toolsetSlug);
  const { addActions, removeActions } = useCommandPalette();
  const location = useLocation();
  const navigate = useNavigate();

  const tools = toolset?.tools ?? [];

  const isExternalMcpProxy = toolset?.kind === "external-mcp-proxy";

  // Get initial tab from URL hash, default depends on toolset kind
  const getTabFromHash = (): ToolsetTabs => {
    const hash = location.hash.slice(1); // Remove the # character
    const validTabs: ToolsetTabs[] = [
      "tools",
      "prompts",
      "resources",
      "auth",
      "mcp",
      "server",
    ];
    return validTabs.includes(hash as ToolsetTabs)
      ? (hash as ToolsetTabs)
      : "tools";
  };

  const [activeTab, setActiveTab] = useState<ToolsetTabs>(getTabFromHash());

  // Update tab when hash changes (e.g., browser back/forward)
  useEffect(() => {
    const newTab = getTabFromHash();
    if (newTab !== activeTab) {
      setActiveTab(newTab);
    }
  }, [location.hash]);

  // Redirect to appropriate default tab based on toolset kind
  useEffect(() => {
    if (!toolset) return;

    if (
      isExternalMcpProxy &&
      (activeTab === "tools" ||
        activeTab === "resources" ||
        activeTab === "prompts")
    ) {
      // External MCP toolsets should show "server" tab instead of tools/resources/prompts
      setActiveTab("server");
      navigate("#server", { replace: true });
    } else if (!isExternalMcpProxy && activeTab === "server") {
      // Default toolsets shouldn't show "server" tab
      setActiveTab("tools");
      navigate("#tools", { replace: true });
    }
  }, [toolset?.kind]);

  // Update URL hash when tab changes
  const handleTabChange = (tab: ToolsetTabs) => {
    setActiveTab(tab);
    navigate(`#${tab}`, { replace: true });
  };
  const [addToolsDialogOpen, setAddToolsDialogOpen] = useState(false);
  const [createToolsetDialogOpen, setCreateToolsetDialogOpen] = useState(false);
  const [addToToolsetDialogOpen, setAddToToolsetDialogOpen] = useState(false);
  const [selectedToolUrns, setSelectedToolUrns] = useState<string[]>([]);
  const [newToolsetName, setNewToolsetName] = useState("");

  useRegisterToolsetTelemetry({
    toolsetSlug: toolsetSlug ?? "",
  });

  const cloneToolset = useCloneToolset();

  // Register page-specific command palette actions
  // Note: routes changes on every render, so we exclude it from deps
  useEffect(() => {
    if (!toolset) return;

    const pageActions = [
      // Only show "Add tools" for default toolsets
      ...(toolset.kind !== "external-mcp-proxy"
        ? [
            {
              id: "toolset-add-tools",
              label: "Add tools",
              icon: "plus",
              onSelect: () => setAddToolsDialogOpen(true),
              group: "Toolset",
            },
          ]
        : []),
      {
        id: "toolset-go-to-playground",
        label: "Open in playground",
        icon: "message-circle",
        onSelect: () => routes.playground.goTo(toolsetSlug),
        group: "Toolset",
      },
      {
        id: "toolset-clone",
        label: "Clone toolset",
        icon: "copy",
        onSelect: () => cloneToolset(toolsetSlug),
        group: "Toolset",
      },
    ];

    addActions(pageActions);

    return () => {
      removeActions(pageActions.map((a) => a.id));
    };
    // addActions and removeActions are memoized in CommandPaletteContext with empty deps
    // so they're stable and don't need to be in the dependency array
  }, [toolsetSlug, toolset?.kind]); // Re-run when toolset slug or kind changes

  // Refetch any loaded instances of this toolset on update (primarily for the playground)
  const refetchInstance = () => {
    const queryKey = queryKeyInstance({
      toolsetSlug,
    });

    queryClient.invalidateQueries({ queryKey });
  };

  const onUpdate = () => {
    refetch?.();
    refetchInstance();
  };

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      telemetry.capture("toolset_event", { action: "toolset_updated" });
      onUpdate();
    },
    onError: (error) => {
      telemetry.capture("toolset_event", {
        action: "toolset_update_failed",
        error: error.message,
      });
    },
  });

  const handleToolUpdate = async (
    tool: Tool,
    updates: { name?: string; description?: string },
  ) => {
    if (tool.type === "prompt") {
      // Prompt templates are only sometimes updated via variation under the hood. We use this endpoint instead to ensure it mirrors editing from the ToolBuilder page itself.
      await client.templates.update({
        updatePromptTemplateForm: {
          ...tool,
          ...updates,
        },
      });
      invalidateTemplate(queryClient, [{ name: tool.name }]);
    } else {
      await client.variations.upsertGlobal({
        upsertGlobalToolVariationForm: {
          ...tool.variation,
          confirm: tool.variation?.confirm as Confirm,
          ...updates,
          srcToolName: tool.canonicalName,
          srcToolUrn: tool.toolUrn,
        },
      });
    }

    telemetry.capture("toolset_event", {
      action: "tool_variation_updated",
      tool_name: tool.name,
      overridden_fields: Object.keys(updates).join(", "),
    });

    onUpdate();
  };

  const handleToolsRemove = useCallback(
    (removedUrns: string[]) => {
      const currentUrns = toolset?.toolUrns || [];
      const updatedUrns = currentUrns.filter(
        (urn) => !removedUrns.includes(urn),
      );

      updateToolsetMutation.mutate(
        {
          request: {
            slug: toolsetSlug,
            updateToolsetRequestBody: {
              toolUrns: updatedUrns,
            },
          },
        },
        {
          onSuccess: () => {
            telemetry.capture("toolset_event", {
              action: "tools_removed",
              count: removedUrns.length,
            });
            toast.success(
              `Removed ${removedUrns.length} tool${removedUrns.length !== 1 ? "s" : ""}`,
            );
          },
          onError: (error) => {
            telemetry.capture("toolset_event", {
              action: "tools_removal_failed",
              error: error.message,
            });
            handleAPIError(error, "Failed to remove tools");
          },
        },
      );
    },
    [toolset?.toolUrns, toolsetSlug],
  );

  const handleTestInPlayground = useCallback(() => {
    routes.playground.goTo(toolsetSlug);
  }, [toolsetSlug]); // routes changes every render but is used in closure

  const handleCreateToolset = useCallback((toolUrns: string[]) => {
    setSelectedToolUrns(toolUrns);
    setCreateToolsetDialogOpen(true);
  }, []);

  const handleAddToToolset = useCallback((toolUrns: string[]) => {
    setSelectedToolUrns(toolUrns);
    setAddToToolsetDialogOpen(true);
  }, []);

  const createToolsetMutation = useCreateToolsetMutation({
    onSuccess: async (data) => {
      telemetry.capture("toolset_event", {
        action: "toolset_created_from_selection",
        toolset_slug: data.slug,
        tool_count: selectedToolUrns.length,
      });

      // Add the selected tools to the new toolset
      await client.toolsets.updateBySlug({
        slug: data.slug,
        updateToolsetRequestBody: {
          toolUrns: selectedToolUrns,
        },
      });

      toast.success(
        `Toolset "${data.name}" created with ${selectedToolUrns.length} tools`,
      );
      setCreateToolsetDialogOpen(false);
      setNewToolsetName("");
      routes.toolsets.toolset.goTo(data.slug);
    },
  });

  const handleCreateToolsetSubmit = () => {
    createToolsetMutation.mutate({
      request: {
        createToolsetRequestBody: {
          name: newToolsetName,
          description: "New Toolset",
        },
      },
    });
  };

  // For now to reduce user confusion we omit server url env variables
  // If a spec already has a security env variable set we will not surface variables as missing for that spec

  const gotoAddTools = () => {
    setAddToolsDialogOpen(true);
  };

  const deleteToolset = useDeleteToolset({
    onSuccess: () => {
      routes.toolsets.goTo();
    },
  });

  const actions = (
    <Stack direction="horizontal" gap={2} align="center">
      {!isExternalMcpProxy && (
        <Button onClick={gotoAddTools} size="sm">
          <Button.LeftIcon>
            <Icon name="plus" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Add Tools</Button.Text>
        </Button>
      )}
      <MoreActions
        actions={[
          {
            label: "Playground",
            onClick: () => {
              routes.playground.goTo(toolsetSlug);
            },
            icon: "message-circle",
          },
          {
            label: "Delete Toolset",
            onClick: () => {
              deleteToolset(toolsetSlug);
            },
            icon: "trash",
            destructive: true,
          },
        ]}
      />
    </Stack>
  );

  const grouped = useGroupedTools(toolset?.tools ?? []);
  const [selectedGroups, setSelectedGroups] = useState<string[]>(
    grouped.map((group) => group.key),
  );
  const groupFilterItems = grouped.map((group) => ({
    label: group.key,
    value: group.key,
  }));
  const filterButton = (
    <MultiSelect
      options={groupFilterItems}
      defaultValue={groupFilterItems.map((group) => group.value)}
      onValueChange={setSelectedGroups}
      placeholder="Filter tools"
      className="w-fit mb-4 capitalize"
    />
  );

  // Filter tools based on selected groups
  // Map unified Tool[] from toolset to group keys, then filter tools
  const groupedToolNames = new Set(
    grouped
      .filter((group) => selectedGroups.includes(group.key))
      .flatMap((group) => group.tools.map((t) => t.name)),
  );

  let toolsToDisplay = tools.filter((tool) => groupedToolNames.has(tool.name));

  // If no tools are selected, show all tools
  // Mostly a failsafe for if the filtering doesn't work as expected
  if (toolsToDisplay.length === 0) {
    toolsToDisplay = tools;
  }

  return (
    <div className={cn("flex flex-col gap-6", className)}>
      {toolset && (
        <ToolsetAuthAlert
          toolset={toolset}
          onConfigureClick={() => handleTabChange("auth")}
          context={context}
        />
      )}
      <ToolsetHeader toolsetSlug={toolsetSlug} actions={actions} />
      {!isExternalMcpProxy && groupFilterItems.length > 1 && filterButton}
      <Tabs
        value={activeTab}
        onValueChange={(value) => handleTabChange(value as ToolsetTabs)}
        className="h-full relative"
      >
        <TabsList className="mb-4">
          {isExternalMcpProxy ? (
            <TabsTrigger value="server">Server</TabsTrigger>
          ) : (
            <>
              <TabsTrigger value="tools">Tools</TabsTrigger>
              <TabsTrigger value="resources">Resources</TabsTrigger>
              <TabsTrigger value="prompts">Prompts</TabsTrigger>
            </>
          )}
          <TabsTrigger value="auth">Auth</TabsTrigger>
          <TabsTrigger value="mcp">MCP</TabsTrigger>
        </TabsList>
        <TabsContent value="server">
          {toolset && <ServerTabContent toolset={toolset} />}
        </TabsContent>
        <TabsContent value="tools">
          {toolsToDisplay.length > 0 ? (
            <ToolList
              tools={toolsToDisplay}
              toolset={toolset}
              onToolUpdate={handleToolUpdate}
              onToolsRemove={handleToolsRemove}
              onCreateToolset={handleCreateToolset}
              onAddToToolset={handleAddToToolset}
              onTestInPlayground={handleTestInPlayground}
            />
          ) : (
            <ToolsetEmptyState
              toolsetSlug={toolsetSlug}
              onAddTools={gotoAddTools}
            />
          )}
        </TabsContent>
        <TabsContent value="prompts">
          {toolset && (
            <PromptsTabContent
              toolset={toolset}
              updateToolsetMutation={updateToolsetMutation}
            />
          )}
        </TabsContent>
        <TabsContent value="resources">
          {toolset && (
            <ResourcesTabContent
              toolset={toolset}
              updateToolsetMutation={updateToolsetMutation}
            />
          )}
        </TabsContent>
        <TabsContent value="auth">
          {toolset && <ToolsetEnvironmentForm toolset={toolset} />}
        </TabsContent>
        <TabsContent value="mcp">
          {toolset && (
            <Stack gap={6}>
              <Stack
                direction="horizontal"
                align="center"
                justify="space-between"
                gap={2}
              >
                <Heading variant="h2">MCP Server Settings</Heading>
                <MCPEnableButton toolset={toolset} />
              </Stack>
              <MCPDetails toolset={toolset} />
            </Stack>
          )}
        </TabsContent>
      </Tabs>
      {toolset && (
        <AddToolsDialog
          open={addToolsDialogOpen}
          onOpenChange={setAddToolsDialogOpen}
          toolset={toolset}
          onAddTools={async (toolUrns) => {
            const currentUrns = toolset.toolUrns || [];
            const newUrns = [...new Set([...currentUrns, ...toolUrns])];

            await client.toolsets.updateBySlug({
              slug: toolsetSlug,
              updateToolsetRequestBody: {
                toolUrns: newUrns,
              },
            });

            toast.success(
              `Added ${toolUrns.length} tool${toolUrns.length !== 1 ? "s" : ""} to ${toolset.name}`,
            );

            await refetch();
          }}
        />
      )}
      <InputDialog
        open={createToolsetDialogOpen}
        onOpenChange={setCreateToolsetDialogOpen}
        title="Create Toolset"
        description={`Create a new toolset with ${selectedToolUrns.length} selected tool(s)`}
        submitButtonText="Create"
        inputs={{
          label: "Toolset name",
          placeholder: "My new toolset",
          value: newToolsetName,
          onChange: setNewToolsetName,
          onSubmit: handleCreateToolsetSubmit,
          validate: (value) => value.length > 0 && value.length <= 40,
          hint: (value) => (
            <div className="flex justify-between w-full">
              <p className="text-destructive">
                {value.length > 40 && "Must be 40 characters or less"}
              </p>
              <p>{value.length}/40</p>
            </div>
          ),
        }}
      />
      <AddToToolsetDialog
        open={addToToolsetDialogOpen}
        onOpenChange={setAddToToolsetDialogOpen}
        toolUrns={selectedToolUrns}
        currentToolsetSlug={toolsetSlug}
      />
    </div>
  );
}
