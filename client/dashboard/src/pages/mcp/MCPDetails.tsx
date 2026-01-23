import { Block, BlockInner } from "@/components/block";
import { CodeBlock } from "@/components/code";
import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { ConfigForm } from "@/components/mcp_install_page/config_form";
import { Page } from "@/components/page-layout";
import { ServerEnableDialog } from "@/components/server-enable-dialog";
import { MCPHeroIllustration } from "@/components/sources/SourceCardIllustrations";
import { ToolList } from "@/components/tool-list";
import { BigToggle } from "@/components/ui/big-toggle";
import { Combobox } from "@/components/ui/combobox";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { InputAndMultiselect } from "@/components/ui/InputAndMultiselect";
import { Label } from "@/components/ui/label";
import { Link } from "@/components/ui/link";
import { MultiSelect } from "@/components/ui/multi-select";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { TextArea } from "@/components/ui/textarea";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useListTools, useToolset } from "@/hooks/toolTypes";
import { useToolsetEnvVars } from "@/hooks/useToolsetEnvVars";
import { useCustomDomain, useMcpUrl } from "@/hooks/useToolsetUrl";
import { isHttpTool, Tool, Toolset, useGroupedTools } from "@/lib/toolTypes";
import { cn, getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Confirm, ToolsetEntry } from "@gram/client/models/components";
import {
  invalidateAllGetPeriodUsage,
  invalidateAllListEnvironments,
  invalidateAllToolset,
  invalidateGetMcpMetadata,
  invalidateTemplate,
  useAddExternalOAuthServerMutation,
  useAddOAuthProxyServerMutation,
  useGetMcpMetadata,
  useLatestDeployment,
  useListEnvironments,
  useRemoveOAuthServerMutation,
  useUpdateEnvironmentMutation,
  useUpdateSecurityVariableDisplayNameMutation,
  useUpdateToolsetMutation,
} from "@gram/client/react-query";
import { Badge, Button, Grid, Icon, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  Check,
  CheckCircleIcon,
  ChevronDown,
  Eye,
  EyeOff,
  Globe,
  LockIcon,
  Pencil,
  Plus,
  Trash2,
  X,
  XCircleIcon
} from "lucide-react";
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { Outlet, useParams } from "react-router";
import { toast } from "sonner";
import { EnvironmentDropdown } from "../environments/EnvironmentDropdown";
import { onboardingStepStorageKeys } from "../home/Home";
import { AddToolsDialog } from "../toolsets/AddToolsDialog";
import { ToolsetEmptyState } from "../toolsets/ToolsetEmptyState";

export function MCPDetailsRoot() {
  return <Outlet />;
}

export function MCPDetailPage() {
  const { toolsetSlug } = useParams();
  const routes = useRoutes();
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();

  const { data: toolset, isLoading } = useToolset(toolsetSlug);
  const { data: deploymentResult, refetch: refetchDeployment } =
    useLatestDeployment();
  const deployment = deploymentResult?.deployment;

  // Call hooks before any conditional returns
  const { url: mcpUrl } = useMcpUrl(toolset);

  const isOAuthConnected = !!(
    toolset?.oauthProxyServer || toolset?.externalOauthServer
  );
  const [isOAuthModalOpen, setIsOAuthModalOpen] = useState(false);
  const [isGramOAuthModalOpen, setIsGramOAuthModalOpen] = useState(false);
  const [isOAuthDetailsModalOpen, setIsOAuthDetailsModalOpen] = useState(false);

  const handleDeleteMcpServer = async () => {
    if (!toolset) return;

    if (
      !confirm(
        "Are you sure you want to delete this MCP server? This action cannot be undone.",
      )
    ) {
      return;
    }

    // Navigate immediately, show loading toast
    routes.mcp.goTo();
    const toastId = toast.loading("Deleting MCP server...");

    console.log(
      "Deleting toolset:",
      toolset.slug,
      "toolUrns:",
      toolset.toolUrns,
    );

    try {
      // Check if this toolset uses an external MCP from the catalog
      const externalMcpUrn = toolset.toolUrns?.find((urn) =>
        urn.includes(":externalmcp:"),
      );

      if (externalMcpUrn && deployment) {
        // Extract the external MCP slug from the URN (format: tools:externalmcp:{slug}:proxy)
        const parts = externalMcpUrn.split(":");
        const externalMcpSlug = parts[2];

        if (externalMcpSlug) {
          // Remove the external MCP from the deployment
          await client.deployments.evolveDeployment({
            evolveForm: {
              deploymentId: deployment.id,
              excludeExternalMcps: [externalMcpSlug],
            },
          });
        }
      }

      // Delete the toolset
      await client.toolsets.deleteBySlug({ slug: toolset.slug });

      telemetry.capture("mcp_event", {
        action: "mcp_server_deleted",
        slug: toolset.slug,
      });

      invalidateAllToolset(queryClient);
      invalidateAllGetPeriodUsage(queryClient);
      refetchDeployment();

      console.log("Successfully deleted toolset:", toolset.slug);
      toast.success("MCP server deleted", { id: toastId });
    } catch (error) {
      console.error("Failed to delete MCP server:", error);
      toast.error(
        `Failed to delete: ${error instanceof Error ? error.message : "Unknown error"}`,
        { id: toastId },
      );
    }
  };

  useEffect(() => {
    localStorage.setItem(onboardingStepStorageKeys.configure, "true");
  }, []);

  // Calculate if there are missing required env vars for the tab indicator
  // Must be before early return to avoid hooks order issues
  const missingRequiredEnvVars = useMemo(() => {
    if (!toolset) return 0;
    let count = 0;
    // Count security variables
    toolset.securityVariables?.forEach((secVar) => {
      secVar.envVariables.forEach((envVar) => {
        if (!envVar.toLowerCase().includes("token_url")) {
          count++;
        }
      });
    });
    // Count server variables
    toolset.serverVariables?.forEach((serverVar) => {
      count += serverVar.envVariables.length;
    });
    // Count function environment variables
    count += toolset.functionEnvironmentVariables?.length || 0;
    return count;
  }, [toolset]);

  // TODO: better loading state
  if (isLoading || !toolset) {
    return <div>Loading...</div>;
  }

  const availableOAuthAuthCode =
    toolset?.oauthEnablementMetadata?.oauth2SecurityCount > 0;

  let statusBadge = null;
  if (!toolset.mcpEnabled) {
    statusBadge = (
      <Badge variant="neutral">
        <Badge.LeftIcon>
          <XCircleIcon className="w-3 h-3" />
        </Badge.LeftIcon>
        <Badge.Text>Disabled</Badge.Text>
      </Badge>
    );
  } else if (toolset.mcpIsPublic) {
    statusBadge = (
      <Badge variant="neutral">
        <Badge.LeftIcon>
          <CheckCircleIcon className="w-3 h-3 text-green-600" />
        </Badge.LeftIcon>
        <Badge.Text>Public</Badge.Text>
      </Badge>
    );
  } else {
    statusBadge = (
      <Badge variant="neutral">
        <Badge.LeftIcon>
          <LockIcon className="w-3 h-3" />
        </Badge.LeftIcon>
        <Badge.Text>Private</Badge.Text>
      </Badge>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body fullWidth noPadding>
        {/* Hero Header with Animation - full width */}
        <div className="relative w-full h-64 overflow-hidden">
          <MCPHeroIllustration
            mcpUrl={mcpUrl || ""}
            toolsetSlug={toolset.slug}
          />

          {/* Overlay content */}
          <div className="absolute inset-0 bg-gradient-to-t from-background/80 via-background/40 to-transparent" />
          <div className="absolute bottom-0 left-0 right-0 px-8 py-8 max-w-[1270px] mx-auto w-full">
            <Stack gap={2}>
              <div className="flex items-center gap-3 ml-1">
                <Heading variant="h1" className="text-foreground">
                  {toolset.name}
                </Heading>
                {statusBadge}
              </div>
              <div className="flex items-center gap-2 ml-1">
                <Type muted className="max-w-2xl truncate">
                  {mcpUrl}
                </Type>
                <Button
                  variant="tertiary"
                  size="sm"
                  onClick={() => {
                    if (mcpUrl) {
                      navigator.clipboard.writeText(mcpUrl);
                      toast.success("URL copied to clipboard");
                    }
                  }}
                  className="shrink-0"
                >
                  <Button.LeftIcon>
                    <svg
                      xmlns="http://www.w3.org/2000/svg"
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    >
                      <rect width="14" height="14" x="8" y="8" rx="2" ry="2" />
                      <path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" />
                    </svg>
                  </Button.LeftIcon>
                  <Button.Text className="sr-only">Copy URL</Button.Text>
                </Button>
              </div>
            </Stack>
          </div>

          {/* Action buttons */}
          <div className="absolute top-6 left-0 right-0 px-8 max-w-[1270px] mx-auto w-full">
            <Stack direction="horizontal" gap={2} className="justify-end">
              <Tooltip>
                <TooltipTrigger asChild>
                  {!toolset?.mcpEnabled ||
                  (toolset.mcpIsPublic && !availableOAuthAuthCode) ? (
                    <span className="inline-block">
                      <Button variant="secondary" size="md" disabled={true}>
                        {isOAuthConnected
                          ? "OAuth Connected"
                          : "Configure OAuth"}
                      </Button>
                    </span>
                  ) : (
                    <Button
                      variant="secondary"
                      size="md"
                      onClick={() =>
                        isOAuthConnected
                          ? setIsOAuthDetailsModalOpen(true)
                          : toolset.mcpIsPublic
                            ? setIsOAuthModalOpen(true)
                            : setIsGramOAuthModalOpen(true)
                      }
                    >
                      {isOAuthConnected ? "OAuth Connected" : "Configure OAuth"}
                    </Button>
                  )}
                </TooltipTrigger>
                {(!toolset?.mcpEnabled ||
                  (toolset.mcpIsPublic && !availableOAuthAuthCode)) && (
                  <TooltipContent>
                    {!toolset?.mcpEnabled
                      ? "Enable server to configure OAuth"
                      : "This MCP server does not require the OAuth authorization code flow"}
                  </TooltipContent>
                )}
              </Tooltip>
              <MCPEnableButton toolset={toolset} />
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="secondary"
                    size="md"
                    onClick={handleDeleteMcpServer}
                  >
                    <Button.LeftIcon>
                      <Trash2 className="w-4 h-4" />
                    </Button.LeftIcon>
                    <Button.Text className="sr-only">
                      Delete MCP server
                    </Button.Text>
                  </Button>
                </TooltipTrigger>
                <TooltipContent>Delete MCP server</TooltipContent>
              </Tooltip>
            </Stack>
          </div>
        </div>

        {/* Sub-navigation tabs */}
        <Tabs defaultValue="overview" className="w-full flex-1 flex flex-col">
          <div className="border-b">
            <div className="max-w-[1270px] mx-auto px-8">
              <TabsList className="h-auto bg-transparent p-0 gap-6 rounded-none">
                <TabsTrigger
                  value="overview"
                  className="relative h-11 px-1 pb-3 pt-3 bg-transparent! rounded-none border-none shadow-none! text-muted-foreground data-[state=active]:text-foreground data-[state=active]:bg-transparent! after:absolute after:bottom-0 after:left-0 after:right-0 after:h-0.5 after:bg-transparent data-[state=active]:after:bg-primary"
                >
                  Overview
                </TabsTrigger>
                <TabsTrigger
                  value="tools"
                  className="relative h-11 px-1 pb-3 pt-3 bg-transparent! rounded-none border-none shadow-none! text-muted-foreground data-[state=active]:text-foreground data-[state=active]:bg-transparent! after:absolute after:bottom-0 after:left-0 after:right-0 after:h-0.5 after:bg-transparent data-[state=active]:after:bg-primary"
                >
                  Tools
                </TabsTrigger>
                <TabsTrigger
                  value="settings"
                  className="relative h-11 px-1 pb-3 pt-3 bg-transparent! rounded-none border-none shadow-none! text-muted-foreground data-[state=active]:text-foreground data-[state=active]:bg-transparent! after:absolute after:bottom-0 after:left-0 after:right-0 after:h-0.5 after:bg-transparent data-[state=active]:after:bg-primary"
                >
                  Settings
                </TabsTrigger>
                <TabsTrigger
                  value="authentication"
                  className="relative h-11 px-1 pb-3 pt-3 bg-transparent! rounded-none border-none shadow-none! text-muted-foreground data-[state=active]:text-foreground data-[state=active]:bg-transparent! after:absolute after:bottom-0 after:left-0 after:right-0 after:h-0.5 after:bg-transparent data-[state=active]:after:bg-primary"
                >
                  <span className="flex items-center gap-1.5">
                    Authentication
                    {missingRequiredEnvVars > 0 && (
                      <AlertTriangle className="h-3.5 w-3.5 text-warning" />
                    )}
                  </span>
                </TabsTrigger>
              </TabsList>
            </div>
          </div>

          {/* Tab Content */}
          <div className="max-w-[1270px] mx-auto px-8 py-8 w-full">
            <TabsContent value="overview" className="mt-0 w-full">
              <MCPOverviewTab toolset={toolset} />
            </TabsContent>

            <TabsContent value="tools" className="mt-0 w-full">
              <MCPToolsTab toolset={toolset} />
            </TabsContent>

            <TabsContent value="settings" className="mt-0 w-full">
              <MCPSettingsTab toolset={toolset} />
            </TabsContent>

            <TabsContent value="authentication" className="mt-0 w-full">
              <MCPAuthenticationTab toolset={toolset} />
            </TabsContent>
          </div>
        </Tabs>

        <ConnectOAuthModal
          isOpen={isOAuthModalOpen}
          onClose={() => setIsOAuthModalOpen(false)}
          toolsetSlug={toolset.slug}
          toolset={toolset}
        />
        <GramOAuthProxyModal
          isOpen={isGramOAuthModalOpen}
          onClose={() => setIsGramOAuthModalOpen(false)}
          toolset={toolset}
        />
        <OAuthDetailsModal
          isOpen={isOAuthDetailsModalOpen}
          onClose={() => setIsOAuthDetailsModalOpen(false)}
          toolset={toolset}
        />
      </Page.Body>
    </Page>
  );
}

export function MCPEnableButton({ toolset }: { toolset: Toolset }) {
  const queryClient = useQueryClient();
  const [isServerEnableDialogOpen, setIsServerEnableDialogOpen] =
    useState(false);
  const updateToolsetMutation = useUpdateToolsetMutation();
  const telemetry = useTelemetry();
  const handleServerEnabledToggle = () => {
    updateToolsetMutation.mutate(
      {
        request: {
          slug: toolset.slug,
          updateToolsetRequestBody: { mcpEnabled: !toolset.mcpEnabled },
        },
      },
      {
        onSuccess: () => {
          invalidateAllToolset(queryClient);
          invalidateAllGetPeriodUsage(queryClient);

          telemetry.capture("mcp_event", {
            action: toolset.mcpEnabled ? "mcp_disabled" : "mcp_enabled",
            slug: toolset.slug,
          });
          toast.success(
            toolset.mcpEnabled ? "MCP server disabled" : "MCP server enabled",
          );
        },
      },
    );
  };

  return (
    <>
      <Button
        variant="secondary"
        onClick={() => setIsServerEnableDialogOpen(true)}
      >
        {toolset.mcpEnabled ? "ENABLED" : "ENABLE"}
      </Button>
      <ServerEnableDialog
        isOpen={isServerEnableDialogOpen}
        onClose={() => setIsServerEnableDialogOpen(false)}
        onConfirm={handleServerEnabledToggle}
        isLoading={updateToolsetMutation.isPending}
        currentlyEnabled={toolset.mcpEnabled ?? false}
      />
    </>
  );
}

/**
 * Overview Tab - Hosted URL and Installation instructions
 */
function MCPOverviewTab({ toolset }: { toolset: Toolset }) {
  const { url: mcpUrl } = useMcpUrl(toolset);

  return (
    <Stack className="mb-4">
      <PageSection
        heading="Hosted URL"
        description="The URL you or your users will use to access this MCP server."
      >
        <CodeBlock className="mb-2">{mcpUrl ?? ""}</CodeBlock>
      </PageSection>

      <PageSection
        heading="MCP Installation"
        description="Share this page with your users to give simple instructions for getting started with your MCP in their client like Cursor or Claude Desktop."
      >
        {!toolset.mcpIsPublic && (
          <Type small italic destructive>
            Your server is private. To share with external users, you must make
            it public.
          </Type>
        )}
        <Stack className="mt-2" gap={1}>
          <ConfigForm toolset={toolset} />
        </Stack>
      </PageSection>
    </Stack>
  );
}

/**
 * Tools Tab - Manage tools in the MCP server
 */
function MCPToolsTab({ toolset }: { toolset: Toolset }) {
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const client = useSdkClient();
  const routes = useRoutes();
  const { data: fullToolset, refetch } = useToolset(toolset.slug);

  const [addToolsDialogOpen, setAddToolsDialogOpen] = useState(false);

  const tools = fullToolset?.tools ?? [];

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      telemetry.capture("toolset_event", { action: "toolset_updated" });
      refetch();
      invalidateAllToolset(queryClient);
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

    refetch();
  };

  const handleToolsRemove = useCallback(
    (removedUrns: string[]) => {
      const currentUrns = fullToolset?.toolUrns || [];
      const updatedUrns = currentUrns.filter(
        (urn) => !removedUrns.includes(urn),
      );

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
            telemetry.capture("toolset_event", {
              action: "tools_removed",
              count: removedUrns.length,
            });
            toast.success(
              `Removed ${removedUrns.length} tool${removedUrns.length !== 1 ? "s" : ""}`,
            );
          },
        },
      );
    },
    [fullToolset?.toolUrns, toolset.slug],
  );

  const handleTestInPlayground = useCallback(() => {
    routes.playground.goTo(toolset.slug);
  }, [toolset.slug]);

  // Group filtering
  const grouped = useGroupedTools(tools);
  const [selectedGroups, setSelectedGroups] = useState<string[]>(
    grouped.map((group) => group.key),
  );

  // Update selected groups when grouped changes
  useEffect(() => {
    setSelectedGroups(grouped.map((group) => group.key));
  }, [grouped.length]);

  const groupFilterItems = grouped.map((group) => ({
    label: group.key,
    value: group.key,
  }));

  // Filter tools based on selected groups
  const groupedToolNames = new Set(
    grouped
      .filter((group) => selectedGroups.includes(group.key))
      .flatMap((group) => group.tools.map((t) => t.name)),
  );

  let toolsToDisplay = tools.filter((tool) => groupedToolNames.has(tool.name));
  if (toolsToDisplay.length === 0) {
    toolsToDisplay = tools;
  }

  return (
    <Stack className="mb-4">
      {/* Header with Add Tools button */}
      <Stack
        direction="horizontal"
        justify="space-between"
        align="center"
        className="mb-4"
      >
        <Heading variant="h3">Tools</Heading>
        <Button onClick={() => setAddToolsDialogOpen(true)} size="sm">
          <Button.LeftIcon>
            <Icon name="plus" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Add Tools</Button.Text>
        </Button>
      </Stack>

      {/* Group filter */}
      {groupFilterItems.length > 1 && (
        <MultiSelect
          options={groupFilterItems}
          defaultValue={groupFilterItems.map((group) => group.value)}
          onValueChange={setSelectedGroups}
          placeholder="Filter tools"
          className="w-fit mb-4 capitalize"
        />
      )}

      {/* Tools list or empty state */}
      {toolsToDisplay.length > 0 ? (
        <ToolList
          tools={toolsToDisplay}
          toolset={fullToolset}
          onToolUpdate={handleToolUpdate}
          onToolsRemove={handleToolsRemove}
          onTestInPlayground={handleTestInPlayground}
        />
      ) : (
        <ToolsetEmptyState
          toolsetSlug={toolset.slug}
          onAddTools={() => setAddToolsDialogOpen(true)}
        />
      )}

      {/* Add Tools Dialog */}
      {fullToolset && (
        <AddToolsDialog
          open={addToolsDialogOpen}
          onOpenChange={setAddToolsDialogOpen}
          toolset={fullToolset}
          onAddTools={async (toolUrns) => {
            const currentUrns = fullToolset.toolUrns || [];
            const newUrns = [...new Set([...currentUrns, ...toolUrns])];

            await client.toolsets.updateBySlug({
              slug: toolset.slug,
              updateToolsetRequestBody: {
                toolUrns: newUrns,
              },
            });

            toast.success(
              `Added ${toolUrns.length} tool${toolUrns.length !== 1 ? "s" : ""} to ${toolset.name}`,
            );

            await refetch();
            invalidateAllToolset(queryClient);
          }}
        />
      )}
    </Stack>
  );
}

/**
 * Environment Variable type for the Environments tab
 */
interface EnvironmentVariable {
  id: string;
  key: string;
  // Track multiple values per variable - each value can be in different environments
  valueGroups: Array<{
    valueHash: string;
    value: string; // Redacted value for display
    environments: string[]; // Environment slugs that have this value
  }>;
  isUserProvided: boolean;
  isRequired: boolean; // True for advertised vars from toolset, false for custom user-added
  description?: string; // Optional description for required vars
  createdAt?: Date;
  updatedAt?: Date;
}

/**
 * Environments Tab - Vercel-style environment variables management
 */
function MCPAuthenticationTab({ toolset }: { toolset: Toolset }) {
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();

  const { data: environmentsData } = useListEnvironments();
  const environments = environmentsData?.environments ?? [];

  // State for the list of environment variables
  const [envVars, setEnvVars] = useState<EnvironmentVariable[]>([]);
  const [isAddingNew, setIsAddingNew] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [filterEnvironment, setFilterEnvironment] = useState<string>("all");
  const [visibleVars, setVisibleVars] = useState<Set<string>>(new Set());
  const [selectedEnvironmentView, setSelectedEnvironmentView] = useState<string | null>(null);

  // Initialize selectedEnvironmentView to default environment when environments load
  useEffect(() => {
    if (environments.length > 0 && !selectedEnvironmentView && toolset.defaultEnvironmentSlug) {
      const defaultEnv = environments.find(e => e.slug === toolset.defaultEnvironmentSlug);
      if (defaultEnv) {
        setSelectedEnvironmentView(toolset.defaultEnvironmentSlug);
      }
    }
  }, [environments, toolset.defaultEnvironmentSlug, selectedEnvironmentView]);

  // Clear editing state when environment view changes
  useEffect(() => {
    setEditingState(new Map());
  }, [selectedEnvironmentView]);

  // New variable form state
  const [newKey, setNewKey] = useState("");
  const [newValue, setNewValue] = useState("");
  const [newTargetEnvironments, setNewTargetEnvironments] = useState<string[]>(
    [],
  );
  const [newIsUserProvided, setNewIsUserProvided] = useState(false);
  const [newValueVisible, setNewValueVisible] = useState(false);

  // Edit variable state
  const [editingVar, setEditingVar] = useState<EnvironmentVariable | null>(
    null,
  );
  const [editValue, setEditValue] = useState("");
  const [editTargetEnvironments, setEditTargetEnvironments] = useState<
    string[]
  >([]);
  const [editValueVisible, setEditValueVisible] = useState(false);

  // Track editing state for required variables (value and target environments)
  type EditingState = { value: string; targetEnvironments: string[] };
  const [editingState, setEditingState] = useState<Map<string, EditingState>>(
    new Map(),
  );

  // Update environment mutation
  const updateEnvironmentMutation = useUpdateEnvironmentMutation({
    onSuccess: () => {
      invalidateAllListEnvironments(queryClient);
      telemetry.capture("environment_event", {
        action: "environment_variable_updated",
        toolset_slug: toolset.slug,
      });
    },
  });

  // Load existing environment variables from toolset
  useEffect(() => {
    const existingVars: EnvironmentVariable[] = [];
    const envMap = new Map<string, string[]>();
    const requiredVarNames = new Set<string>();

    // Helper to build value groups for a variable across all environments
    const getValueGroups = (varName: string) => {
      const valueHashMap = new Map<
        string,
        { value: string; environments: string[] }
      >();

      environments.forEach((env) => {
        const entry = env.entries.find((e) => e.name === varName);
        if (entry) {
          if (!valueHashMap.has(entry.valueHash)) {
            valueHashMap.set(entry.valueHash, {
              value: entry.value,
              environments: [env.slug],
            });
          } else {
            valueHashMap.get(entry.valueHash)!.environments.push(env.slug);
          }
        }
      });

      return Array.from(valueHashMap.entries()).map(
        ([valueHash, { value, environments }]) => ({
          valueHash,
          value,
          environments,
        }),
      );
    };

    // Get env vars from security variables (these are required auth credentials)
    toolset.securityVariables?.forEach((secVar) => {
      secVar.envVariables.forEach((envVar) => {
        if (!envVar.toLowerCase().includes("token_url")) {
          requiredVarNames.add(envVar);
          const valueGroups = getValueGroups(envVar);
          const id = `sec-${secVar.id}-${envVar}`;
          existingVars.push({
            id,
            key: envVar,
            valueGroups,
            isUserProvided: true,
            isRequired: true,
            description: `Authentication credential for ${secVar.name || "API access"}`,
            createdAt: new Date(),
          });
          // Initialize the environments map with the most common value's environments
          if (valueGroups.length > 0) {
            const mostCommonGroup = valueGroups.reduce((prev, current) =>
              current.environments.length > prev.environments.length
                ? current
                : prev,
            );
            envMap.set(id, mostCommonGroup.environments);
          }
        }
      });
    });

    // Get env vars from server variables (these are required server config)
    toolset.serverVariables?.forEach((serverVar) => {
      serverVar.envVariables.forEach((envVar) => {
        requiredVarNames.add(envVar);
        const valueGroups = getValueGroups(envVar);
        const id = `srv-${envVar}`;
        existingVars.push({
          id,
          key: envVar,
          valueGroups,
          isUserProvided: false,
          isRequired: true,
          description: "Server configuration variable",
          createdAt: new Date(),
        });
        // Initialize the environments map with the most common value's environments
        if (valueGroups.length > 0) {
          const mostCommonGroup = valueGroups.reduce((prev, current) =>
            current.environments.length > prev.environments.length
              ? current
              : prev,
          );
          envMap.set(id, mostCommonGroup.environments);
        }
      });
    });

    // Get env vars from function environment variables (these are required for functions)
    toolset.functionEnvironmentVariables?.forEach((funcVar) => {
      requiredVarNames.add(funcVar.name);
      const valueGroups = getValueGroups(funcVar.name);
      const id = `func-${funcVar.name}`;
      existingVars.push({
        id,
        key: funcVar.name,
        valueGroups,
        isUserProvided: false,
        isRequired: true,
        description: funcVar.description || "Function environment variable",
        createdAt: new Date(),
      });
      // Initialize the environments map with the most common value's environments
      if (valueGroups.length > 0) {
        const mostCommonGroup = valueGroups.reduce((prev, current) =>
          current.environments.length > prev.environments.length
            ? current
            : prev,
        );
        envMap.set(id, mostCommonGroup.environments);
      }
    });

    // Load custom variables from environments (variables not in the required list)
    const customVarMap = new Map<
      string,
      {
        valueGroups: Map<string, { value: string; environments: Set<string> }>;
        createdAt: Date;
      }
    >();

    environments.forEach((env) => {
      env.entries.forEach((entry) => {
        // Skip if this is a required variable or a token_url
        if (
          !requiredVarNames.has(entry.name) &&
          !entry.name.toLowerCase().includes("token_url")
        ) {
          if (!customVarMap.has(entry.name)) {
            customVarMap.set(entry.name, {
              valueGroups: new Map([
                [
                  entry.valueHash,
                  { value: entry.value, environments: new Set([env.slug]) },
                ],
              ]),
              createdAt: entry.createdAt,
            });
          } else {
            const varData = customVarMap.get(entry.name)!;
            if (!varData.valueGroups.has(entry.valueHash)) {
              varData.valueGroups.set(entry.valueHash, {
                value: entry.value,
                environments: new Set([env.slug]),
              });
            } else {
              varData.valueGroups.get(entry.valueHash)!.environments.add(env.slug);
            }
          }
        }
      });
    });

    // Add custom variables to the list
    customVarMap.forEach((info, varName) => {
      const id = `custom-${varName}`;
      const valueGroups = Array.from(info.valueGroups.entries()).map(
        ([valueHash, { value, environments }]) => ({
          valueHash,
          value,
          environments: Array.from(environments),
        }),
      );
      existingVars.push({
        id,
        key: varName,
        valueGroups,
        isUserProvided: false,
        isRequired: false,
        description: "Custom environment variable",
        createdAt: info.createdAt,
      });
    });

    setEnvVars(existingVars);
  }, [toolset.slug, environments]);

  const handleAddVariable = () => {
    if (!newKey.trim()) return;

    // If no environments are explicitly selected, use all environments
    const targetEnvs =
      newTargetEnvironments.length > 0
        ? newTargetEnvironments
        : environments.map((e) => e.slug);

    // Save to selected environments
    // Don't add to envVars state - it will be reloaded from environments after save
    const varKey = newKey.toUpperCase().replace(/\s+/g, "_");
    if (!newIsUserProvided && newValue && targetEnvs.length > 0) {
      targetEnvs.forEach((envSlug) => {
        updateEnvironmentMutation.mutate({
          request: {
            slug: envSlug,
            updateEnvironmentRequestBody: {
              entriesToUpdate: [{ name: varKey, value: newValue }],
              entriesToRemove: [],
            },
          },
        });
      });
    }
    setNewKey("");
    setNewValue("");
    setNewTargetEnvironments([]);
    setNewIsUserProvided(false);
    setNewValueVisible(false);
    setIsAddingNew(false);

    telemetry.capture("environment_event", {
      action: "environment_variable_added",
      toolset_slug: toolset.slug,
      is_user_provided: newIsUserProvided,
    });
  };

  const handleDeleteVariable = (id: string) => {
    const envVar = envVars.find((v) => v.id === id);
    if (!envVar) return;

    // Delete from all environments that have this variable
    const allEnvs = getAllEnvironments(envVar);
    allEnvs.forEach((envSlug) => {
      updateEnvironmentMutation.mutate({
        request: {
          slug: envSlug,
          updateEnvironmentRequestBody: {
            entriesToUpdate: [],
            entriesToRemove: [envVar.key],
          },
        },
      });
    });

    telemetry.capture("environment_event", {
      action: "environment_variable_deleted",
      toolset_slug: toolset.slug,
    });
  };

  const toggleVisibility = (id: string) => {
    setVisibleVars((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  // Helper functions for working with valueGroups
  const getAllEnvironments = (envVar: EnvironmentVariable): string[] => {
    const allEnvs = new Set<string>();
    envVar.valueGroups.forEach((group) => {
      group.environments.forEach((env) => allEnvs.add(env));
    });
    return Array.from(allEnvs);
  };

  const getPrimaryValue = (envVar: EnvironmentVariable): string => {
    if (envVar.valueGroups.length === 0) return "";
    // Return the value from the most common group
    const mostCommonGroup = envVar.valueGroups.reduce((prev, current) =>
      current.environments.length > prev.environments.length ? current : prev,
    );
    return mostCommonGroup.value;
  };

  const hasValue = (envVar: EnvironmentVariable): boolean => {
    return envVar.valueGroups.length > 0 && envVar.valueGroups.some(g => g.environments.length > 0);
  };

  // Check if an environment has a value for a specific variable
  const environmentHasValue = (envVar: EnvironmentVariable, environmentSlug: string): boolean => {
    if (envVar.isUserProvided) return true;
    return envVar.valueGroups.some(group => group.environments.includes(environmentSlug));
  };

  // Get the value for a variable in a specific environment
  const getValueForEnvironment = (envVar: EnvironmentVariable, environmentSlug: string): string => {
    const group = envVar.valueGroups.find(g => g.environments.includes(environmentSlug));
    return group?.value || "";
  };

  // Separate required and custom variables
  const requiredVars = envVars.filter((v) => v.isRequired);
  const customVars = envVars.filter((v) => !v.isRequired);

  // Check if an environment has all required variables configured
  const environmentHasAllRequiredVariables = (environmentSlug: string): boolean => {
    return requiredVars.every(v => environmentHasValue(v, environmentSlug));
  };

  // Filter variables
  const filteredRequiredVars = requiredVars.filter((v) => {
    const matchesSearch = v.key
      .toLowerCase()
      .includes(searchQuery.toLowerCase());
    const allEnvs = getAllEnvironments(v);
    const matchesEnv =
      filterEnvironment === "all" ||
      allEnvs.includes(filterEnvironment);
    return matchesSearch && matchesEnv;
  });

  const filteredCustomVars = customVars.filter((v) => {
    const matchesSearch = v.key
      .toLowerCase()
      .includes(searchQuery.toLowerCase());
    const allEnvs = getAllEnvironments(v);
    const matchesEnv =
      filterEnvironment === "all" ||
      allEnvs.includes(filterEnvironment);
    return matchesSearch && matchesEnv;
  });

  // Count missing required variables (user-provided ones count as configured)
  const missingRequiredCount = requiredVars.filter(
    (v) => !hasValue(v) && !v.isUserProvided,
  ).length;

  // Handle value change for required variables
  const handleValueChange = (id: string, newValue: string) => {
    const envVar = envVars.find((v) => v.id === id);
    if (!envVar) return;

    // Get current or default target environments
    let targetEnvironments: string[];
    if (editingState.has(id)) {
      targetEnvironments = editingState.get(id)!.targetEnvironments;
    } else {
      // Check if the currently viewed environment has a value
      const currentEnvHasValue = selectedEnvironmentView
        ? envVar.valueGroups.some(g => g.environments.includes(selectedEnvironmentView))
        : false;

      // If viewing a specific environment and it doesn't have a value, default to all environments
      if (selectedEnvironmentView && !currentEnvHasValue) {
        targetEnvironments = environments.map((e) => e.slug);
      } else if (envVar.valueGroups.length > 0) {
        // If variable has values, use the most common group
        const mostCommonGroup = envVar.valueGroups.reduce((prev, current) =>
          current.environments.length > prev.environments.length
            ? current
            : prev,
        );
        targetEnvironments = mostCommonGroup.environments;
      } else {
        // If completely unset, use all environments
        targetEnvironments = environments.map((e) => e.slug);
      }
    }

    setEditingState(
      new Map(editingState.set(id, { value: newValue, targetEnvironments })),
    );
  };

  // Get editing value for a variable (either from editing state or from valueGroups)
  const getEditingValue = (envVar: EnvironmentVariable): string => {
    if (editingState.has(envVar.id)) {
      return editingState.get(envVar.id)!.value;
    }
    // If viewing a specific environment, show that environment's value
    if (selectedEnvironmentView) {
      return getValueForEnvironment(envVar, selectedEnvironmentView);
    }
    return getPrimaryValue(envVar);
  };

  // Get selected environments for a variable
  const getSelectedEnvironments = (id: string): string[] => {
    // If actively editing, use the editing state
    if (editingState.has(id)) {
      return editingState.get(id)!.targetEnvironments;
    }

    // Otherwise, show which environments have the currently displayed value
    const envVar = envVars.find(v => v.id === id);
    if (!envVar) return environments.map((e) => e.slug);

    // If viewing a specific environment, find which environments have that same value
    if (selectedEnvironmentView) {
      const viewedValue = getValueForEnvironment(envVar, selectedEnvironmentView);
      if (viewedValue) {
        const matchingGroup = envVar.valueGroups.find(g => g.value === viewedValue);
        if (matchingGroup) {
          return matchingGroup.environments;
        }
      }
      // If the current environment has no value, default to all environments
      return environments.map((e) => e.slug);
    }

    // Default to showing environments with the primary value
    const primaryValue = getPrimaryValue(envVar);
    if (primaryValue) {
      const matchingGroup = envVar.valueGroups.find(g => g.value === primaryValue);
      if (matchingGroup) {
        return matchingGroup.environments;
      }
    }

    return environments.map((e) => e.slug);
  };

  // Update selected environments for a variable
  const setSelectedEnvironments = (id: string, envs: string[]) => {
    const current = editingState.get(id);
    if (current) {
      setEditingState(
        new Map(editingState.set(id, { ...current, targetEnvironments: envs })),
      );
    } else {
      // Initialize editing state with current value and new environments
      const envVar = envVars.find(v => v.id === id);
      const value = envVar ? getEditingValue(envVar) : "";
      setEditingState(
        new Map(editingState.set(id, { value, targetEnvironments: envs })),
      );
    }
  };

  // Get environments with different values (for indeterminate checkbox state)
  const getIndeterminateEnvironments = (id: string): string[] => {
    const envVar = envVars.find(v => v.id === id);
    if (!envVar) return [];

    const currentValue = getEditingValue(envVar);
    const selectedEnvs = getSelectedEnvironments(id);

    // Find environments that have a value different from the current value
    // and are not already selected
    return environments
      .filter(env => {
        // Skip if already selected
        if (selectedEnvs.includes(env.slug)) return false;

        // Get the value for this environment
        const envValue = getValueForEnvironment(envVar, env.slug);

        // Include if this environment has a value and it's different from current
        return envValue && envValue !== currentValue;
      })
      .map(env => env.slug);
  };

  // Toggle user-provided state for a variable
  const handleToggleUserProvided = (id: string) => {
    setEnvVars(
      envVars.map((v) =>
        v.id === id ? { ...v, isUserProvided: !v.isUserProvided } : v,
      ),
    );
    // Clear editing state when toggling
    const newEditingState = new Map(editingState);
    newEditingState.delete(id);
    setEditingState(newEditingState);
  };

  // Save a required variable
  const handleSaveVariable = (envVar: EnvironmentVariable) => {
    const value = getEditingValue(envVar);
    if (!value) return;

    // Use selected environments from state
    const targetEnvs = getSelectedEnvironments(envVar.id);

    if (targetEnvs.length === 0) {
      toast.error("No environments selected");
      return;
    }

    targetEnvs.forEach((envSlug) => {
      updateEnvironmentMutation.mutate({
        request: {
          slug: envSlug,
          updateEnvironmentRequestBody: {
            entriesToUpdate: [{ name: envVar.key, value }],
            entriesToRemove: [],
          },
        },
      });
    });

    // Clear editing state after save
    const newEditingState = new Map(editingState);
    newEditingState.delete(envVar.id);
    setEditingState(newEditingState);

    toast.success(`Saved ${envVar.key} to ${targetEnvs.length} environment${targetEnvs.length > 1 ? "s" : ""}`);

    telemetry.capture("environment_event", {
      action: "required_variable_configured",
      toolset_slug: toolset.slug,
      variable_key: envVar.key,
    });
  };

  const getEnvironmentLabel = (envVar: EnvironmentVariable) => {
    const allEnvs = getAllEnvironments(envVar);
    if (allEnvs.length === 0) return "No Environments";
    if (allEnvs.length === environments.length) return "All Environments";
    if (allEnvs.length === 1) {
      return (
        environments.find((e) => e.slug === allEnvs[0])?.name || allEnvs[0]
      );
    }
    return `${allEnvs.length} Environments`;
  };

  const formatDate = (date?: Date) => {
    if (!date) return "";
    const month = date.getMonth() + 1;
    const day = date.getDate();
    const year = date.getFullYear().toString().slice(-2);
    return `${month}/${day}/${year}`;
  };

  const environmentSwitcher = useMemo(() => {
    return environments.length > 0 && (
      <Combobox
        items={environments.map(env => ({
          value: env.slug,
          label: env.name,
          icon: environmentHasAllRequiredVariables(env.slug) ? (
            <CheckCircleIcon className="w-4 h-4 text-green-600 mr-2" />
          ) : (
            <XCircleIcon className="w-4 h-4 text-muted-foreground/50 mr-2" />
          ),
        }))}
        selected={selectedEnvironmentView || undefined}
        onSelectionChange={(item) => setSelectedEnvironmentView(item.value)}
        variant="outline"
        className="min-w-[200px]"
      >
        <Type variant="small">
          {environments.find(e => e.slug === selectedEnvironmentView)?.name || "Select Environment"}
        </Type>
      </Combobox>
    )
  }, [environments, selectedEnvironmentView, requiredVars, envVars]);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">
            Environment Variables
          </h2>
          <p className="text-sm text-muted-foreground mt-1">
            Configure required credentials and add custom variables.
          </p>
        </div>
       {environmentSwitcher}
      </div>

      {/* Required Configuration Section */}
      {requiredVars.length > 0 && (
        <div className="space-y-4">
          <div className="flex items-center gap-3">
            <h3 className="text-lg font-medium">Required Configuration</h3>
            {missingRequiredCount > 0 && (
              <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-xs text-muted-foreground">
                {missingRequiredCount} not configured
              </span>
            )}
            {missingRequiredCount === 0 && requiredVars.length > 0 && (
              <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-xs text-muted-foreground">
                <span className="w-1.5 h-1.5 rounded-full bg-green-500" />
                All configured
              </span>
            )}
          </div>
          <p className="text-sm text-muted-foreground">
            These variables are required by this MCP server. Set their values to
            enable full functionality.
          </p>

          <div className="border rounded-lg overflow-hidden">
            {filteredRequiredVars.map((envVar, index) => (
              <div
                key={envVar.id}
                className={cn(
                  "grid grid-cols-[auto_1fr_auto] gap-4 items-center px-5 py-4 transition-colors",
                  index !== filteredRequiredVars.length - 1 && "border-b",
                )}
              >
                {/* Status indicator */}
                <div>
                  {selectedEnvironmentView
                    ? (environmentHasValue(envVar, selectedEnvironmentView) ? (
                        <div className="w-2 h-2 rounded-full bg-green-500" />
                      ) : (
                        <div className="w-2 h-2 rounded-full bg-muted-foreground/30" />
                      ))
                    : (hasValue(envVar) || envVar.isUserProvided ? (
                        <div className="w-2 h-2 rounded-full bg-green-500" />
                      ) : (
                        <div className="w-2 h-2 rounded-full bg-muted-foreground/30" />
                      ))
                  }
                </div>

                {/* Variable Info */}
                <div className="min-w-0">
                  <div className="font-medium font-mono text-sm truncate">
                    {envVar.key}
                  </div>
                  {envVar.description && (
                    <div className="text-xs text-muted-foreground mt-0.5 truncate">
                      {envVar.description}
                    </div>
                  )}
                </div>

                {/* Right side: Toggle + Value + Environments + Save */}
                <div className="flex items-center gap-4">
                  {/* User provided toggle */}
                  <label className="flex items-center gap-2 cursor-pointer">
                    <Switch
                      checked={envVar.isUserProvided}
                      onCheckedChange={() => handleToggleUserProvided(envVar.id)}
                    />
                    <span className="text-xs text-muted-foreground whitespace-nowrap">
                      User provided
                    </span>
                  </label>

                  {/* Value Input or Runtime badge with dropdown */}
                  <div className="w-56">
                    {envVar.isUserProvided ? (
                      <div className="h-9 flex items-center px-3 rounded-md bg-muted text-xs text-muted-foreground font-mono">
                        Set at runtime
                      </div>
                    ) : (
                      <InputAndMultiselect
                        value={getEditingValue(envVar)}
                        onChange={(value) => handleValueChange(envVar.id, value)}
                        selectedOptions={getSelectedEnvironments(envVar.id)}
                        indeterminateOptions={getIndeterminateEnvironments(envVar.id)}
                        onSelectedOptionsChange={(selected) =>
                          setSelectedEnvironments(envVar.id, selected)
                        }
                        options={environments.map((env) => ({
                          value: env.slug,
                          label: env.name,
                        }))}
                        placeholder="Enter value..."
                        type="password"
                      />
                    )}
                  </div>

                  {/* Save button - always visible for consistent width */}
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => handleSaveVariable(envVar)}
                    disabled={!editingState.has(envVar.id) || !editingState.get(envVar.id)?.value || envVar.isUserProvided}
                    className={envVar.isUserProvided ? "invisible" : ""}
                  >
                    Save
                  </Button>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Add New Variable Sheet */}
      <Sheet open={isAddingNew} onOpenChange={setIsAddingNew}>
        <SheetContent
          side="right"
          className="w-[500px] sm:max-w-[500px] flex flex-col"
        >
          <SheetHeader className="px-6 pt-6 pb-0">
            <SheetTitle className="text-lg font-semibold">
              Add Environment Variable
            </SheetTitle>
          </SheetHeader>

          <div className="flex-1 overflow-y-auto px-6 py-6 space-y-6">
            {/* Key and Value inputs side by side */}
            <div className="flex gap-4">
              <div className="flex-1">
                <Label className="text-xs text-muted-foreground mb-1.5 block">
                  Key
                </Label>
                <input
                  type="text"
                  value={newKey}
                  onChange={(e) => setNewKey(e.target.value.toUpperCase())}
                  placeholder="CLIENT_KEY..."
                  className="w-full h-10 px-3 rounded-md border border-input bg-background text-sm font-mono placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </div>
              <div className="flex-1">
                <Label className="text-xs text-muted-foreground mb-1.5 block">
                  Value
                </Label>
                <input
                  type={newValueVisible ? "text" : "password"}
                  value={newValue}
                  onChange={(e) => setNewValue(e.target.value)}
                  placeholder=""
                  disabled={newIsUserProvided}
                  className="w-full h-10 px-3 rounded-md border border-input bg-background text-sm font-mono placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring disabled:bg-muted disabled:cursor-not-allowed"
                />
              </div>
            </div>

            {/* Add Note link */}
            <button className="text-sm text-muted-foreground hover:text-foreground transition-colors">
              Add Note
            </button>

            {/* Add Another button */}
            <button className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors">
              <Plus className="h-4 w-4" />
              Add Another
            </button>

            {/* Environments section */}
            <div className="pt-4 border-t">
              <Label className="text-xs text-muted-foreground mb-2 block">
                Environments
              </Label>
              <Popover>
                <PopoverTrigger asChild>
                  <button className="w-full h-10 px-3 rounded-md border border-input bg-background text-sm flex items-center justify-between hover:bg-accent transition-colors">
                    <div className="flex items-center gap-2">
                      <svg
                        className="h-4 w-4 text-muted-foreground"
                        fill="none"
                        stroke="currentColor"
                        viewBox="0 0 24 24"
                        strokeWidth="2"
                      >
                        <rect x="3" y="3" width="18" height="6" rx="1" />
                        <rect
                          x="3"
                          y="11"
                          width="18"
                          height="6"
                          rx="1"
                          opacity="0.5"
                        />
                      </svg>
                      <span>
                        {newTargetEnvironments.length === 0 ||
                        newTargetEnvironments.length === environments.length
                          ? "All Environments"
                          : newTargetEnvironments.length === 1
                            ? environments.find(
                                (e) => e.slug === newTargetEnvironments[0],
                              )?.name || newTargetEnvironments[0]
                            : `${newTargetEnvironments.length} Environments`}
                      </span>
                    </div>
                    <ChevronDown className="h-4 w-4 text-muted-foreground" />
                  </button>
                </PopoverTrigger>
                <PopoverContent align="start" className="w-[352px] p-1">
                  <div
                    className="px-3 py-2 text-sm rounded-sm cursor-pointer hover:bg-accent flex items-center gap-2"
                    onClick={() =>
                      setNewTargetEnvironments(environments.map((e) => e.slug))
                    }
                  >
                    <div
                      className={cn(
                        "w-4 h-4 rounded-sm border flex items-center justify-center",
                        newTargetEnvironments.length === 0 ||
                          newTargetEnvironments.length === environments.length
                          ? "bg-primary border-primary text-primary-foreground"
                          : "border-border",
                      )}
                    >
                      {(newTargetEnvironments.length === 0 ||
                        newTargetEnvironments.length === environments.length) && (
                        <Check className="h-3 w-3" />
                      )}
                    </div>
                    All Environments
                  </div>
                  {environments.map((env) => (
                    <div
                      key={env.slug}
                      className="px-3 py-2 text-sm rounded-sm cursor-pointer hover:bg-accent flex items-center gap-2"
                      onClick={() => {
                        if (newTargetEnvironments.includes(env.slug)) {
                          setNewTargetEnvironments(
                            newTargetEnvironments.filter((s) => s !== env.slug),
                          );
                        } else {
                          setNewTargetEnvironments([
                            ...newTargetEnvironments,
                            env.slug,
                          ]);
                        }
                      }}
                    >
                      <div
                        className={cn(
                          "w-4 h-4 rounded-sm border flex items-center justify-center",
                          newTargetEnvironments.includes(env.slug)
                            ? "bg-primary border-primary text-primary-foreground"
                            : "border-border",
                        )}
                      >
                        {newTargetEnvironments.includes(env.slug) && (
                          <Check className="h-3 w-3" />
                        )}
                      </div>
                      {env.name}
                    </div>
                  ))}
                </PopoverContent>
              </Popover>
            </div>

            {/* Sensitive toggle */}
            <div className="flex items-center justify-between pt-4">
              <div className="flex items-center gap-3">
                <Switch
                  checked={newIsUserProvided}
                  onCheckedChange={setNewIsUserProvided}
                />
                <div>
                  <span className="text-sm font-medium">Sensitive</span>
                  <span className="text-xs text-yellow-600 ml-2">⚡</span>
                  <p className="text-xs text-muted-foreground">
                    Available for Production and Preview only
                  </p>
                </div>
              </div>
            </div>
          </div>

          <SheetFooter className="px-6 py-4 border-t flex-row justify-between items-center">
            <button className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground transition-colors">
              <svg
                className="h-4 w-4"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
                strokeWidth="2"
              >
                <path d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
              </svg>
              Import .env
            </button>
            <span className="text-xs text-muted-foreground">
              or paste .env contents in Key input
            </span>
            <Button
              onClick={() => {
                handleAddVariable();
                setIsAddingNew(false);
              }}
              disabled={!newKey.trim()}
            >
              Save
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      {/* Edit Variable Sheet */}
      <Sheet
        open={editingVar !== null}
        onOpenChange={(open) => {
          if (!open) {
            setEditingVar(null);
            setEditValue("");
            setEditTargetEnvironments([]);
            setEditValueVisible(false);
          }
        }}
      >
        <SheetContent
          side="right"
          className="w-[500px] sm:max-w-[500px] flex flex-col"
        >
          <SheetHeader className="px-6 pt-6 pb-0">
            <SheetTitle className="text-lg font-semibold">
              Edit Environment Variable
            </SheetTitle>
          </SheetHeader>

          <div className="flex-1 overflow-y-auto px-6 py-6 space-y-6">
            {/* Key (read-only) and Value inputs side by side */}
            <div className="flex gap-4">
              <div className="flex-1">
                <Label className="text-xs text-muted-foreground mb-1.5 block">
                  Key
                </Label>
                <input
                  type="text"
                  value={editingVar?.key || ""}
                  disabled
                  className="w-full h-10 px-3 rounded-md border border-input bg-muted text-sm font-mono cursor-not-allowed"
                />
              </div>
              <div className="flex-1">
                <Label className="text-xs text-muted-foreground mb-1.5 block">
                  Value
                </Label>
                <div className="relative">
                  <input
                    type={editValueVisible ? "text" : "password"}
                    value={editValue}
                    onChange={(e) => setEditValue(e.target.value)}
                    placeholder="Enter value..."
                    disabled={editingVar?.isUserProvided}
                    className="w-full h-10 px-3 pr-10 rounded-md border border-input bg-background text-sm font-mono placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring disabled:bg-muted disabled:cursor-not-allowed"
                  />
                  <button
                    type="button"
                    onClick={() => setEditValueVisible(!editValueVisible)}
                    className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  >
                    {editValueVisible ? (
                      <EyeOff className="h-4 w-4" />
                    ) : (
                      <Eye className="h-4 w-4" />
                    )}
                  </button>
                </div>
              </div>
            </div>

            {/* Environments section */}
            <div className="pt-4 border-t">
              <Label className="text-xs text-muted-foreground mb-2 block">
                Environments
              </Label>
              <Popover>
                <PopoverTrigger asChild>
                  <button className="w-full h-10 px-3 rounded-md border border-input bg-background text-sm flex items-center justify-between hover:bg-accent transition-colors">
                    <div className="flex items-center gap-2">
                      <svg
                        className="h-4 w-4 text-muted-foreground"
                        fill="none"
                        stroke="currentColor"
                        viewBox="0 0 24 24"
                        strokeWidth="2"
                      >
                        <rect x="3" y="3" width="18" height="6" rx="1" />
                        <rect
                          x="3"
                          y="11"
                          width="18"
                          height="6"
                          rx="1"
                          opacity="0.5"
                        />
                      </svg>
                      <span>
                        {editTargetEnvironments.length === 0
                          ? "Select Environments"
                          : editTargetEnvironments.length === environments.length
                            ? "All Environments"
                            : editTargetEnvironments.length === 1
                              ? environments.find(
                                  (e) => e.slug === editTargetEnvironments[0],
                                )?.name || editTargetEnvironments[0]
                              : `${editTargetEnvironments.length} Environments`}
                      </span>
                    </div>
                    <ChevronDown className="h-4 w-4 text-muted-foreground" />
                  </button>
                </PopoverTrigger>
                <PopoverContent align="start" className="w-[352px] p-1">
                  <div
                    className="px-3 py-2 text-sm rounded-sm cursor-pointer hover:bg-accent flex items-center gap-2"
                    onClick={() =>
                      setEditTargetEnvironments(environments.map((e) => e.slug))
                    }
                  >
                    <div
                      className={cn(
                        "w-4 h-4 rounded-sm border flex items-center justify-center",
                        editTargetEnvironments.length === environments.length
                          ? "bg-primary border-primary text-primary-foreground"
                          : "border-border",
                      )}
                    >
                      {editTargetEnvironments.length === environments.length && (
                        <Check className="h-3 w-3" />
                      )}
                    </div>
                    All Environments
                  </div>
                  {environments.map((env) => (
                    <div
                      key={env.slug}
                      className="px-3 py-2 text-sm rounded-sm cursor-pointer hover:bg-accent flex items-center gap-2"
                      onClick={() => {
                        if (editTargetEnvironments.includes(env.slug)) {
                          setEditTargetEnvironments(
                            editTargetEnvironments.filter((s) => s !== env.slug),
                          );
                        } else {
                          setEditTargetEnvironments([
                            ...editTargetEnvironments,
                            env.slug,
                          ]);
                        }
                      }}
                    >
                      <div
                        className={cn(
                          "w-4 h-4 rounded-sm border flex items-center justify-center",
                          editTargetEnvironments.includes(env.slug)
                            ? "bg-primary border-primary text-primary-foreground"
                            : "border-border",
                        )}
                      >
                        {editTargetEnvironments.includes(env.slug) && (
                          <Check className="h-3 w-3" />
                        )}
                      </div>
                      {env.name}
                    </div>
                  ))}
                </PopoverContent>
              </Popover>
            </div>

            {editingVar?.isUserProvided && (
              <div className="bg-yellow-50 dark:bg-yellow-950/20 border border-yellow-200 dark:border-yellow-900 rounded-md p-3">
                <p className="text-xs text-yellow-800 dark:text-yellow-200">
                  This is a sensitive variable. Values are provided at runtime.
                </p>
              </div>
            )}
          </div>

          <SheetFooter className="px-6 py-4 border-t flex-row justify-end items-center gap-2">
            <Button
              variant="secondary"
              onClick={() => {
                setEditingVar(null);
                setEditValue("");
                setEditTargetEnvironments([]);
                setEditValueVisible(false);
              }}
            >
              Cancel
            </Button>
            <Button
              onClick={() => {
                if (!editingVar || (!editValue && !editingVar.isUserProvided))
                  return;

                // Save to selected environments
                if (
                  !editingVar.isUserProvided &&
                  editValue &&
                  editTargetEnvironments.length > 0
                ) {
                  editTargetEnvironments.forEach((envSlug) => {
                    updateEnvironmentMutation.mutate({
                      request: {
                        slug: envSlug,
                        updateEnvironmentRequestBody: {
                          entriesToUpdate: [
                            { name: editingVar.key, value: editValue },
                          ],
                          entriesToRemove: [],
                        },
                      },
                    });
                  });

                  // Update the local state
                  setEnvVars(
                    envVars.map((v) =>
                      v.id === editingVar.id
                        ? {
                            ...v,
                            value: editValue,
                            targetEnvironments: editTargetEnvironments,
                            updatedAt: new Date(),
                          }
                        : v,
                    ),
                  );

                  toast.success(`Updated ${editingVar.key}`);

                  telemetry.capture("environment_event", {
                    action: "environment_variable_updated",
                    toolset_slug: toolset.slug,
                  });
                }

                // Close the sheet
                setEditingVar(null);
                setEditValue("");
                setEditTargetEnvironments([]);
                setEditValueVisible(false);
              }}
              disabled={
                !editValue && !editingVar?.isUserProvided ||
                editTargetEnvironments.length === 0
              }
            >
              Save
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      {/* Custom Variables Section */}
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="text-lg font-medium">Custom Variables</h3>
          <Button onClick={() => setIsAddingNew(true)} disabled={isAddingNew}>
            <Button.Text>Add Custom Variable</Button.Text>
          </Button>
        </div>
        <p className="text-sm text-muted-foreground">
          Add your own environment variables for additional configuration.
        </p>

        {/* Filters */}
        <div className="flex items-center gap-3">
          <div className="relative flex-1 max-w-xs">
            <svg
              className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <circle cx="11" cy="11" r="8" />
              <path d="m21 21-4.35-4.35" />
            </svg>
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search..."
              className="w-full h-10 pl-9 pr-3 rounded-md border border-input bg-background text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>
          <Popover>
            <PopoverTrigger asChild>
              <button className="h-10 px-4 rounded-md border border-input bg-background text-sm flex items-center gap-2 hover:bg-accent transition-colors">
                <span>
                  {filterEnvironment === "all"
                    ? "All Environments"
                    : environments.find((e) => e.slug === filterEnvironment)
                        ?.name || filterEnvironment}
                </span>
                <ChevronDown className="h-4 w-4 text-muted-foreground" />
              </button>
            </PopoverTrigger>
            <PopoverContent align="start" className="w-[200px] p-1">
              <div
                className="px-3 py-2 text-sm rounded-sm cursor-pointer hover:bg-accent"
                onClick={() => setFilterEnvironment("all")}
              >
                All Environments
              </div>
              {environments.map((env) => (
                <div
                  key={env.slug}
                  className="px-3 py-2 text-sm rounded-sm cursor-pointer hover:bg-accent"
                  onClick={() => setFilterEnvironment(env.slug)}
                >
                  {env.name}
                </div>
              ))}
            </PopoverContent>
          </Popover>
        </div>

        {/* Custom Variables List */}
        {filteredCustomVars.length > 0 ? (
          <div className="border rounded-lg overflow-hidden">
            {filteredCustomVars.map((envVar, index) => (
              <div
                key={envVar.id}
                className={cn(
                  "flex items-center px-5 py-4 hover:bg-muted/40 transition-colors",
                  index !== filteredCustomVars.length - 1 && "border-b",
                )}
              >
                {/* Code Icon */}
                <div className="w-10 h-10 rounded-full border border-border flex items-center justify-center text-muted-foreground shrink-0 mr-4">
                  <svg
                    width="16"
                    height="16"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  >
                    <polyline points="16 18 22 12 16 6" />
                    <polyline points="8 6 2 12 8 18" />
                  </svg>
                </div>

                {/* Variable Info */}
                <div className="flex-1 min-w-0 mr-4">
                  <div className="font-medium font-mono text-sm">
                    {envVar.key}
                  </div>
                  <div className="text-sm text-muted-foreground">
                    {getEnvironmentLabel(envVar)}
                  </div>
                </div>

                {/* Value with visibility toggle */}
                <div className="flex items-center gap-3 mr-6">
                  <button
                    onClick={() => toggleVisibility(envVar.id)}
                    className="text-muted-foreground hover:text-foreground transition-colors"
                  >
                    {visibleVars.has(envVar.id) ? (
                      <EyeOff className="h-4 w-4" />
                    ) : (
                      <Eye className="h-4 w-4" />
                    )}
                  </button>
                  <span className="font-mono text-sm text-muted-foreground w-28">
                    {envVar.isUserProvided
                      ? "runtime"
                      : visibleVars.has(envVar.id) && hasValue(envVar)
                        ? getPrimaryValue(envVar)
                        : "••••••••••••••"}
                  </span>
                </div>

                {/* Date */}
                <div className="text-sm text-muted-foreground w-28 text-right mr-4">
                  {envVar.updatedAt
                    ? `Updated ${formatDate(envVar.updatedAt)}`
                    : `Added ${formatDate(envVar.createdAt)}`}
                </div>

                {/* Gradient Circle */}
                <div
                  className="w-6 h-6 rounded-full shrink-0 mr-4"
                  style={{
                    background: `linear-gradient(135deg, hsl(${(envVar.key.charCodeAt(0) * 15) % 360}, 80%, 65%), hsl(${(envVar.key.charCodeAt(0) * 15 + 90) % 360}, 70%, 55%))`,
                  }}
                />

                {/* More Actions */}
                <Popover>
                  <PopoverTrigger asChild>
                    <button className="text-muted-foreground hover:text-foreground transition-colors p-1">
                      <svg
                        width="16"
                        height="16"
                        viewBox="0 0 24 24"
                        fill="currentColor"
                      >
                        <circle cx="12" cy="5" r="2" />
                        <circle cx="12" cy="12" r="2" />
                        <circle cx="12" cy="19" r="2" />
                      </svg>
                    </button>
                  </PopoverTrigger>
                  <PopoverContent align="end" className="w-[160px] p-1">
                    <div
                      className="px-3 py-2 text-sm rounded-sm cursor-pointer hover:bg-accent flex items-center gap-2"
                      onClick={() => {
                        // Find the most common value group (the one with the most environments)
                        let mostCommonGroup = envVar.valueGroups[0];
                        envVar.valueGroups.forEach((group) => {
                          if (
                            group.environments.length >
                            mostCommonGroup.environments.length
                          ) {
                            mostCommonGroup = group;
                          }
                        });

                        setEditingVar(envVar);
                        setEditValue(
                          mostCommonGroup ? mostCommonGroup.value : "",
                        );
                        setEditTargetEnvironments(
                          mostCommonGroup ? mostCommonGroup.environments : [],
                        );
                        setEditValueVisible(false);
                      }}
                    >
                      <Pencil className="h-4 w-4" />
                      Edit
                    </div>
                    <div
                      className="px-3 py-2 text-sm rounded-sm cursor-pointer hover:bg-accent flex items-center gap-2"
                      onClick={() => toggleVisibility(envVar.id)}
                    >
                      <Eye className="h-4 w-4" />
                      {visibleVars.has(envVar.id) ? "Hide value" : "Show value"}
                    </div>
                    <div
                      className="px-3 py-2 text-sm rounded-sm cursor-pointer hover:bg-destructive hover:text-destructive-foreground flex items-center gap-2 text-destructive"
                      onClick={() => handleDeleteVariable(envVar.id)}
                    >
                      <Trash2 className="h-4 w-4" />
                      Delete
                    </div>
                  </PopoverContent>
                </Popover>
              </div>
            ))}
          </div>
        ) : (
          // Empty State for custom variables
          <div className="border rounded-lg border-dashed p-8 text-center">
            <p className="text-muted-foreground mb-4">
              {searchQuery || filterEnvironment !== "all"
                ? "No custom variables match your filters."
                : "No custom environment variables added yet."}
            </p>
            {!searchQuery && filterEnvironment === "all" && (
              <Button onClick={() => setIsAddingNew(true)} variant="secondary">
                <Button.LeftIcon>
                  <Plus className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Add Custom Variable</Button.Text>
              </Button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

/**
 * Settings Tab - Visibility, Slug, Custom Domain, Tool Selection Mode
 */
function MCPSettingsTab({ toolset }: { toolset: Toolset }) {
  const telemetry = useTelemetry();
  const queryClient = useQueryClient();
  const session = useSession();
  const { orgSlug } = useParams();
  const { domain } = useCustomDomain();
  const routes = useRoutes();

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      invalidateAllToolset(queryClient);
      toast.success("MCP settings saved successfully");
      telemetry.capture("mcp_event", {
        action: "mcp_settings_saved",
        slug: toolset.slug,
        isPublic: mcpIsPublic,
      });
    },
    onError: (error) => {
      if (
        error.message &&
        error.message.includes(
          "maximum number of public MCP servers for your account type",
        )
      ) {
        setIsMaxServersModalOpen(true);
      }

      // Discard staged changes
      setMcpSlug(toolset.mcpSlug || "");
      setMcpIsPublic(toolset.mcpIsPublic);
    },
  });

  const [mcpSlug, setMcpSlug] = useState(toolset.mcpSlug || "");
  const [mcpIsPublic, setMcpIsPublic] = useState(toolset.mcpIsPublic);
  const [isCustomDomainModalOpen, setIsCustomDomainModalOpen] = useState(false);
  const [isMaxServersModalOpen, setIsMaxServersModalOpen] = useState(false);

  const mcpSlugError = useMcpSlugValidation(mcpSlug, toolset.mcpSlug);

  const { url: _mcpUrl, customServerURL } = useMcpUrl(toolset);

  const handleMcpSlugChange = (value: string) => {
    value = value.slice(0, 40);
    setMcpSlug(value);
  };

  const linkDomainButton = domain && domain.activated && domain.verified && (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="secondary"
          size="sm"
          className="mr-2"
          disabled={updateToolsetMutation.isPending}
          onClick={() => {
            updateToolsetMutation.mutate({
              request: {
                slug: toolset.slug,
                updateToolsetRequestBody: {
                  customDomainId: domain.id,
                  mcpSlug: mcpSlug,
                },
              },
            });
          }}
        >
          Link Domain
        </Button>
      </TooltipTrigger>
      <TooltipContent>{domain.domain}</TooltipContent>
    </Tooltip>
  );

  const customDomain =
    domain && session.gramAccountType !== "free" && !toolset.customDomainId ? (
      linkDomainButton
    ) : (
      <Button
        variant="secondary"
        size="sm"
        onClick={() => {
          if (session.gramAccountType == "free") {
            setIsCustomDomainModalOpen(true);
          } else {
            routes.settings.goTo();
          }
        }}
      >
        Configure
      </Button>
    );

  const anyChanges = mcpSlug !== toolset.mcpSlug;

  const saveButton = anyChanges && (
    <Button
      onClick={() => {
        updateToolsetMutation.mutate({
          request: {
            slug: toolset.slug,
            updateToolsetRequestBody: {
              mcpSlug: mcpSlug,
              mcpIsPublic,
            },
          },
        });
      }}
      size="sm"
      disabled={!!mcpSlugError || !mcpSlug || !anyChanges}
    >
      Save
    </Button>
  );

  const discardButton = anyChanges && (
    <Button
      variant="tertiary"
      size="sm"
      onClick={() => {
        setMcpSlug(toolset.mcpSlug || "");
        setMcpIsPublic(toolset.mcpIsPublic);
      }}
    >
      Discard
    </Button>
  );

  const PublicToggle = ({ isPublic }: { isPublic: boolean }) => {
    const onToggle = (value: string) => {
      const newIsPublic = value === "public";
      setMcpIsPublic(newIsPublic);
      updateToolsetMutation.mutate({
        request: {
          slug: toolset.slug,
          updateToolsetRequestBody: { mcpIsPublic: newIsPublic },
        },
      });
    };

    return (
      <BigToggle
        options={[
          {
            value: "public",
            icon: "globe",
            label: "Public",
            description:
              "Anyone with the URL can read the tools hosted by this server. Authentication is still required to use the tools.",
          },
          {
            value: "private",
            icon: "lock",
            label: "Private",
            description:
              "Only users with a Gram API Key can read the tools hosted by this server.",
          },
        ]}
        selectedValue={isPublic ? "public" : "private"}
        onSelect={onToggle}
      />
    );
  };

  const ToolSelectionModeToggle = ({
    toolSelectionMode,
  }: {
    toolSelectionMode: string;
  }) => {
    const onToggle = (value: string) => {
      updateToolsetMutation.mutate({
        request: {
          slug: toolset.slug,
          updateToolsetRequestBody: { toolSelectionMode: value },
        },
      });
    };

    return (
      <BigToggle
        align="start"
        options={[
          {
            value: "static",
            icon: "list-ordered",
            label: "Static",
            description:
              "Traditional MCP. Every tool is added into context up front.",
          },
          {
            value: "dynamic",
            icon: "search",
            label: "Dynamic",
            description:
              "Highly token efficient and effective for large toolsets. The LLM can discover tools as it needs them.",
          },
        ]}
        selectedValue={toolSelectionMode}
        onSelect={onToggle}
      />
    );
  };

  return (
    <Stack className="mb-4">
      <PageSection
        heading="Visibility"
        description="Make your MCP server visible to the world, or protected behind a Gram key."
      >
        <PublicToggle isPublic={mcpIsPublic ?? false} />
      </PageSection>

      <PageSection
        heading="Custom Slug"
        description="Customize the URL path for your MCP server."
      >
        <Block label="Slug" error={mcpSlugError} className="p-0">
          <BlockInner>
            <Stack direction="horizontal" align="center">
              <Type
                muted
                mono
                variant="small"
                className="hidden @lg/main:block"
              >
                {toolset.mcpSlug && customServerURL
                  ? `${customServerURL}/mcp/`
                  : `${getServerURL()}/mcp/`}
              </Type>
              {!toolset.customDomainId ? (
                <Input
                  className="border rounded px-2 py-1 w-full"
                  placeholder="Enter MCP Slug"
                  value={mcpSlug}
                  onChange={handleMcpSlugChange}
                  maxLength={40}
                  requiredPrefix={`${orgSlug}-`}
                />
              ) : (
                <Input
                  className="border rounded px-2 py-1 w-full"
                  placeholder="Enter MCP Slug"
                  value={mcpSlug}
                  onChange={handleMcpSlugChange}
                  maxLength={40}
                  disabled={!toolset.customDomainId}
                />
              )}
              <Stack
                direction="horizontal"
                gap={1}
                align="center"
                className="ml-auto"
              >
                {discardButton}
                {saveButton}
              </Stack>
            </Stack>
          </BlockInner>
        </Block>
      </PageSection>

      <PageSection
        heading="Custom Domain"
        description="Host your MCP server at your own domain."
      >
        <Block label="Domain" className="p-0">
          <BlockInner>
            <Stack direction="horizontal" align="center">
              <Type mono small>
                {toolset.mcpSlug && customServerURL
                  ? `${customServerURL}/mcp/`
                  : `http://mcp.your-company.com/`}
              </Type>
              <Type muted mono small>
                {mcpSlug}
              </Type>
              {!toolset.customDomainId && (
                <div className="ml-auto">{customDomain}</div>
              )}
            </Stack>
          </BlockInner>
        </Block>
      </PageSection>

      <PageSection
        heading="Tool Selection Mode"
        featureType="experimental"
        description="Change how this server's tools will be presented to the LLM. Can have drastic effects on context management, especially for larger toolsets. Use with care."
      >
        <ToolSelectionModeToggle
          toolSelectionMode={toolset.toolSelectionMode ?? "static"}
        />
      </PageSection>

      {toolset.securityVariables && toolset.securityVariables.length > 0 && (
        <AuthorizationHeadersSection toolset={toolset} />
      )}

      <FeatureRequestModal
        isOpen={isCustomDomainModalOpen}
        onClose={() => setIsCustomDomainModalOpen(false)}
        title="Host your MCP at a custom domain"
        description="Custom domains require upgrading to a pro account type. Someone should be in touch shortly, or feel free to book a meeting directly."
        actionType="mcp_custom_domain"
        icon={Globe}
        telemetryData={{ slug: toolset.slug }}
        accountUpgrade
      />
      <FeatureRequestModal
        isOpen={isMaxServersModalOpen}
        onClose={() => setIsMaxServersModalOpen(false)}
        title="Public MCP Server Limit Reached"
        description={`You have reached the maximum number of public MCP servers for the ${session.gramAccountType} account type. Someone should be in touch shortly, or feel free to book a meeting directly to upgrade.`}
        actionType="max_public_mcp_servers"
        icon={Globe}
        telemetryData={{ slug: toolset.slug }}
        accountUpgrade
      />
    </Stack>
  );
}

// Keep the old MCPDetails for backward compatibility (can be removed later)
export function MCPDetails({ toolset }: { toolset: Toolset }) {
  return <MCPSettingsTab toolset={toolset} />;
}

function PageSection({
  heading,
  description,
  featureType,
  children,
  className,
}: {
  heading: string;
  description: string;
  fullWidth?: boolean;
  featureType?: "experimental" | "beta";
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <Stack gap={2} className={cn("mb-8", className)}>
      <Heading variant="h3" className="flex items-center">
        {heading}
        {featureType && (
          <Badge variant="warning" className="ml-2">
            {featureType}
          </Badge>
        )}
      </Heading>
      <Type muted small className="max-w-2xl">
        {description}
      </Type>
      {children}
    </Stack>
  );
}

function AuthorizationHeadersSection({ toolset }: { toolset: Toolset }) {
  const queryClient = useQueryClient();
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editValue, setEditValue] = useState("");

  // Fetch MCP metadata to get saved header display names
  // Use throwOnError: false since metadata may not exist for all toolsets
  const { data: mcpMetadata } = useGetMcpMetadata(
    { toolsetSlug: toolset.slug },
    undefined,
    { enabled: !!toolset.slug, throwOnError: false },
  );

  // Get the saved display names map from metadata
  const headerDisplayNames = mcpMetadata?.metadata?.headerDisplayNames ?? {};

  const updateDisplayNameMutation =
    useUpdateSecurityVariableDisplayNameMutation({
      onSuccess: () => {
        invalidateAllToolset(queryClient);
        invalidateGetMcpMetadata(queryClient, [{ toolsetSlug: toolset.slug }]);
        toast.success("Header display name updated");
        setEditingId(null);
        setEditValue("");
      },
      onError: (error) => {
        toast.error(
          error instanceof Error
            ? error.message
            : "Failed to update display name",
        );
      },
    });

  const handleEditStart = (
    id: string,
    currentDisplayName: string | undefined,
    name: string,
  ) => {
    setEditingId(id);
    setEditValue(currentDisplayName || name);
  };

  const handleEditCancel = () => {
    setEditingId(null);
    setEditValue("");
  };

  const handleEditSave = (securityKey: string) => {
    updateDisplayNameMutation.mutate({
      request: {
        updateSecurityVariableDisplayNameRequestBody: {
          displayName: editValue.trim(),
          securityKey: securityKey,
          toolsetSlug: toolset.slug,
        },
      },
    });
  };

  // Flatten security variables to show one row per environment variable
  // This handles cases like basic auth where one securityVariable has multiple envVariables
  const envVarEntries = toolset.securityVariables?.flatMap((secVar) => {
    // Filter out token_url env vars as they're not user-facing
    const filteredEnvVars = secVar.envVariables.filter(
      (envVar) => !envVar.toLowerCase().includes("token_url"),
    );

    // If no env vars after filtering, show the security variable itself
    if (filteredEnvVars.length === 0) {
      return [
        {
          id: secVar.id,
          envVar: secVar.name,
          securityVariableId: secVar.id,
          displayName: headerDisplayNames[secVar.name] || secVar.displayName,
        },
      ];
    }

    // Create one entry per env var, looking up display name from metadata
    return filteredEnvVars.map((envVar, index) => ({
      id: `${secVar.id}-${index}`,
      envVar: envVar,
      securityVariableId: secVar.id,
      // Look up the saved display name from headerDisplayNames map
      displayName: headerDisplayNames[envVar] as string | undefined,
    }));
  });

  return (
    <PageSection
      heading="Authorization Headers"
      description="Customize how authorization headers are displayed to users. These friendly names will appear in MCP clients while the actual header names are used internally."
    >
      <Stack gap={2} className="max-w-2xl">
        {envVarEntries?.map((entry) => (
          <div
            key={entry.id}
            className="bg-stone-100 dark:bg-stone-900 p-1 rounded-md"
          >
            <BlockInner>
              <Stack direction="horizontal" align="center" className="w-full">
                <Stack className="flex-1 min-w-0">
                  <Type small muted className="text-xs">
                    Header: <span className="font-mono">{entry.envVar}</span>
                  </Type>
                  {editingId === entry.id ? (
                    <Stack
                      direction="horizontal"
                      align="center"
                      gap={2}
                      className="mt-1"
                    >
                      <Input
                        value={editValue}
                        onChange={setEditValue}
                        placeholder="Enter display name"
                        className="flex-1"
                        autoFocus
                        onKeyDown={(e: React.KeyboardEvent) => {
                          if (e.key === "Enter") {
                            handleEditSave(entry.envVar);
                          } else if (e.key === "Escape") {
                            handleEditCancel();
                          }
                        }}
                      />
                      <Button
                        variant="tertiary"
                        size="sm"
                        onClick={() => handleEditSave(entry.envVar)}
                        disabled={updateDisplayNameMutation.isPending}
                      >
                        <Check className="w-4 h-4" />
                      </Button>
                      <Button
                        variant="tertiary"
                        size="sm"
                        onClick={handleEditCancel}
                        disabled={updateDisplayNameMutation.isPending}
                      >
                        <X className="w-4 h-4" />
                      </Button>
                    </Stack>
                  ) : (
                    <Stack direction="horizontal" align="center" gap={2}>
                      <Type className="font-medium">
                        {entry.displayName ||
                          entry.envVar.replace(/_/g, "-") ||
                          "Unknown"}
                      </Type>
                      {entry.displayName &&
                        entry.displayName !==
                          entry.envVar.replace(/_/g, "-") && (
                          <Badge variant="neutral" className="text-xs">
                            renamed
                          </Badge>
                        )}
                    </Stack>
                  )}
                </Stack>
                {editingId !== entry.id && (
                  <Button
                    variant="tertiary"
                    size="sm"
                    onClick={() =>
                      handleEditStart(
                        entry.id,
                        entry.displayName,
                        entry.envVar.replace(/_/g, "-"),
                      )
                    }
                  >
                    <Pencil className="w-4 h-4" />
                  </Button>
                )}
              </Stack>
            </BlockInner>
          </div>
        ))}
      </Stack>
    </PageSection>
  );
}

export function MCPJson({
  toolset,
  fullWidth = false,
  className,
}: {
  toolset: ToolsetEntry;
  fullWidth?: boolean; // If true, the code block will take up the full width of the page even when there's only one
  className?: string;
}) {
  const telemetry = useTelemetry();

  const { public: mcpJsonPublic, internal: mcpJsonInternal } =
    useMcpConfigs(toolset);

  const onCopy = () => {
    telemetry.capture("mcp_event", {
      action: "mcp_json_copied",
      slug: toolset.slug,
    });
  };

  return (
    <Grid
      gap={4}
      className={cn("my-4!", className)}
      columns={!fullWidth ? { xs: 1, md: 2, lg: 2, xl: 2, "2xl": 2 } : 1}
    >
      <Grid.Item>
        <Type className="font-medium">Pass-through Authentication</Type>
        <Type muted small className="max-w-3xl mb-2!">
          Pass API credentials directly to the MCP server.
          <br />
          <span
            className={
              !toolset.mcpIsPublic
                ? "font-medium text-warning-foreground"
                : "italic"
            }
          >
            Requires a Gram API key if the server is not public.
          </span>
        </Type>
        <CodeBlock onCopy={onCopy}>{mcpJsonPublic}</CodeBlock>
      </Grid.Item>
      <Grid.Item>
        <Type className="font-medium">Managed Authentication</Type>
        <Type muted small className="max-w-3xl mb-2!">
          Manage API authentication with Gram environments.
          <br />
          Users need a single Gram API Key rather than bringing their own keys.
        </Type>
        <CodeBlock onCopy={onCopy}>{mcpJsonInternal}</CodeBlock>
      </Grid.Item>
    </Grid>
  );
}

export const useMcpConfigs = (toolset: ToolsetEntry | undefined) => {
  const { url: mcpUrl } = useMcpUrl(toolset);
  const { data: tools } = useListTools();

  const toolsetTools = toolset
    ? tools?.tools.filter((tool) => toolset.tools.some((t) => t.id === tool.id))
    : undefined;

  const requiresServerURL =
    toolsetTools?.some((tool) => isHttpTool(tool) && !tool.defaultServerUrl) ??
    false;

  // Get env headers using the existing hook for fallback
  const envHeaders = useToolsetEnvVars(toolset, requiresServerURL).filter(
    (header) => !header.toLowerCase().includes("token_url"),
  );

  if (!toolset) return { public: "", internal: "" };

  // Build header names using display names when available
  // Display names make the config more user-friendly (e.g., "API-Key" instead of "X-RAPIDAPI-KEY")
  const getHeaderNameForMcp = (envVar: string): string => {
    // Find the security variable that has this env var
    const secVar = toolset.securityVariables?.find((sv) =>
      sv.envVariables.some((ev) => ev.toLowerCase() === envVar.toLowerCase()),
    );

    if (secVar?.displayName) {
      // Use display name, normalized for header format
      return secVar.displayName.replace(/\s+/g, "-").replace(/_/g, "-");
    }

    // Fall back to the env var format
    return envVar.replace(/_/g, "-");
  };

  // Build the args array for public MCP config
  const mcpJsonPublicArgs = [
    "mcp-remote@0.1.25",
    mcpUrl,
    ...envHeaders.flatMap((header) => [
      "--header",
      `MCP-${getHeaderNameForMcp(header)}:${"${VALUE}"}`,
    ]),
  ];

  if (!toolset.mcpIsPublic) {
    mcpJsonPublicArgs.push("--header", "Authorization:${GRAM_KEY}");
  }

  // Indent each line of the header args array by 8 spaces for alignment
  const INDENT = " ".repeat(8);
  const argsStringIndented = JSON.stringify(mcpJsonPublicArgs, null, 2)
    .split("\n")
    .map((line, idx) => (idx === 0 ? line : INDENT + line))
    .join("\n");

  const mcpJsonPublic = `{
  "mcpServers": {
    "Gram${toolset.slug
      .replace(/-/g, "")
      .replace(/^./, (c) => c.toUpperCase())}": {
      "command": "npx",
      "args": ${argsStringIndented}${
        !toolset.mcpIsPublic
          ? `,
      "env": {
        "GRAM_KEY": "Bearer <your-key-here>"
      }`
          : ""
      }
    }
  }
}`;

  const mcpJsonInternal = `{
  "mcpServers": {
    "Gram${toolset.slug
      .replace(/-/g, "")
      .replace(/^./, (c) => c.toUpperCase())}": {
      "command": "npx",
      "args": [
        "mcp-remote@0.1.25",
        "${mcpUrl}",
        "--header",
        "Gram-Environment:${toolset.defaultEnvironmentSlug}",
        "--header",
        "Authorization:\${GRAM_KEY}"
      ],
      "env": {
        "GRAM_KEY": "Bearer <your-key-here>"
      }
    }
  }
}`;

  return { public: mcpJsonPublic, internal: mcpJsonInternal };
};

export function useMcpSlugValidation(
  mcpSlug: string | undefined,
  currentSlug?: string,
) {
  const [slugError, setSlugError] = useState<string | null>(null);
  const client = useSdkClient();

  function validateMcpSlug(slug: string) {
    if (!slug) return "MCP Slug is required";
    if (slug.length > 40) return "Must be 40 characters or fewer";
    if (!/^[a-z0-9_-]+$/.test(slug))
      return "Lowercase letters, numbers, _ or - only";
    return null;
  }

  useEffect(() => {
    setSlugError(null);

    if (mcpSlug && mcpSlug !== currentSlug) {
      const validationError = validateMcpSlug(mcpSlug);
      if (validationError) {
        setSlugError(validationError);
        return;
      }
      client.toolsets
        .checkMCPSlugAvailability({ slug: mcpSlug })
        .then((res) => {
          if (res) {
            setSlugError("This slug is already taken");
          }
        });
    }
  }, [mcpSlug]);

  return slugError;
}

export const randSlug = () => {
  const chars = "abcdefghijklmnopqrstuvwxyz0123456789";
  let rand = "";
  for (let i = 0; i < 5; i++) {
    rand += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return rand;
};

function OAuthDetailsModal({
  isOpen,
  onClose,
  toolset,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolset: Toolset;
}) {
  const { url: mcpUrl } = useMcpUrl(toolset);
  const queryClient = useQueryClient();

  const removeOAuthMutation = useRemoveOAuthServerMutation({
    onSuccess: () => {
      invalidateAllToolset(queryClient);
      onClose();
    },
  });

  const isGramOAuth =
    toolset.oauthProxyServer?.oauthProxyProviders?.[0]?.providerType === "gram";

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-w-2xl max-h-[80vh] flex flex-col">
        <Dialog.Header className="shrink-0">
          <Dialog.Title>
            {toolset.externalOauthServer
              ? "External OAuth Configuration"
              : isGramOAuth
                ? "Gram OAuth Configuration"
                : "OAuth Proxy Configuration"}
          </Dialog.Title>
        </Dialog.Header>
        <div className="flex-1 overflow-y-auto">
          <Stack gap={4}>
            {toolset.oauthProxyServer && isGramOAuth && (
              <>
                <div>
                  <Type className="font-medium">Gram OAuth is Active</Type>
                </div>
                <Stack gap={2} className="">
                  <Type className="mb-2">
                    Gram users with access to your organization can use this MCP
                    server.
                  </Type>
                  {toolset.oauthProxyServer.oauthProxyProviders?.[0]
                    ?.environmentSlug && (
                    <div>
                      <Type small className="font-medium text-muted-foreground">
                        Environment:
                      </Type>
                      <CodeBlock className="mt-1">
                        {
                          toolset.oauthProxyServer.oauthProxyProviders[0]
                            .environmentSlug
                        }
                      </CodeBlock>
                    </div>
                  )}
                </Stack>
              </>
            )}
            {toolset.oauthProxyServer && !isGramOAuth && (
              <>
                <div className="flex items-center justify-between">
                  <Type className="font-medium">OAuth Proxy Server</Type>
                  <Button
                    variant="tertiary"
                    size="sm"
                    className="hover:bg-destructive hover:text-white border-none"
                    onClick={() =>
                      removeOAuthMutation.mutate({
                        request: { slug: toolset.slug },
                      })
                    }
                  >
                    <Trash2 className="w-4 h-4 mr-2" />
                    Unlink
                  </Button>
                </div>
                <Stack gap={2} className="pl-4">
                  <div>
                    <Type small className="font-medium text-muted-foreground">
                      Server Slug:
                    </Type>
                    <CodeBlock className="mt-1">
                      {toolset.oauthProxyServer.slug}
                    </CodeBlock>
                  </div>
                </Stack>
              </>
            )}
            {toolset.oauthProxyServer?.oauthProxyProviders?.map(
              (provider) =>
                provider.providerType !== "gram" && (
                  <Stack key={provider.id} gap={2}>
                    <Stack gap={2} className="pl-4">
                      <div>
                        <Type
                          small
                          className="font-medium text-muted-foreground"
                        >
                          Authorization Endpoint:
                        </Type>
                        <CodeBlock className="mt-1">
                          {provider.authorizationEndpoint}
                        </CodeBlock>
                      </div>
                      <div>
                        <Type
                          small
                          className="font-medium text-muted-foreground"
                        >
                          Token Endpoint:
                        </Type>
                        <CodeBlock className="mt-1">
                          {provider.tokenEndpoint}
                        </CodeBlock>
                      </div>
                      {provider.tokenEndpointAuthMethodsSupported &&
                        provider.tokenEndpointAuthMethodsSupported.length >
                          0 && (
                          <div>
                            <Type
                              small
                              className="font-medium text-muted-foreground"
                            >
                              Token Auth Method:
                            </Type>
                            <CodeBlock className="mt-1">
                              {provider.tokenEndpointAuthMethodsSupported.join(
                                ", ",
                              )}
                            </CodeBlock>
                          </div>
                        )}
                      {provider.scopesSupported &&
                        provider.scopesSupported.length > 0 && (
                          <div>
                            <Type
                              small
                              className="font-medium text-muted-foreground"
                            >
                              Supported Scopes:
                            </Type>
                            <CodeBlock className="mt-1">
                              {provider.scopesSupported.join(", ")}
                            </CodeBlock>
                          </div>
                        )}
                      {provider.environmentSlug && (
                        <div>
                          <Type
                            small
                            className="font-medium text-muted-foreground"
                          >
                            Environment:
                          </Type>
                          <CodeBlock className="mt-1">
                            {provider.environmentSlug}
                          </CodeBlock>
                        </div>
                      )}
                    </Stack>
                  </Stack>
                ),
            )}
            {toolset.externalOauthServer && (
              <Stack gap={2}>
                <div className="flex items-center justify-between">
                  <Type className="font-medium">External OAuth Server</Type>
                  <Button
                    variant="tertiary"
                    size="sm"
                    className="text-muted-foreground hover:text-destructive hover:border-destructive"
                    onClick={() =>
                      removeOAuthMutation.mutate({
                        request: { slug: toolset.slug },
                      })
                    }
                  >
                    <Button.LeftIcon>
                      <Trash2 className="w-4 h-4" />
                    </Button.LeftIcon>
                    <Button.Text className="sr-only">Remove OAuth</Button.Text>
                  </Button>
                </div>
                <Stack gap={2} className="pl-4">
                  <div>
                    <Type small className="font-medium text-muted-foreground">
                      External OAuth Server Slug:
                    </Type>
                    <CodeBlock className="mt-1">
                      {toolset.externalOauthServer.slug}
                    </CodeBlock>
                  </div>
                  <div>
                    <Type small className="font-medium text-muted-foreground">
                      OAuth Authorization Server Discovery URL:
                    </Type>
                    <CodeBlock className="mt-1">
                      {mcpUrl
                        ? `${
                            new URL(mcpUrl).origin
                          }/.well-known/oauth-authorization-server/mcp/${
                            toolset.mcpSlug
                          }`
                        : ""}
                    </CodeBlock>
                  </div>
                  <div>
                    <Type small className="font-medium text-muted-foreground">
                      OAuth Authorization Server Metadata:
                    </Type>
                    <CodeBlock className="mt-1">
                      {JSON.stringify(
                        toolset.externalOauthServer.metadata,
                        null,
                        2,
                      )}
                    </CodeBlock>
                  </div>
                </Stack>
              </Stack>
            )}
          </Stack>
        </div>
        {isGramOAuth && (
          <Dialog.Footer>
            <Button variant="tertiary" onClick={onClose}>
              Close
            </Button>
            <Button
              variant="destructive-primary"
              onClick={() =>
                removeOAuthMutation.mutate({
                  request: { slug: toolset.slug },
                })
              }
            >
              <Trash2 className="w-4 h-4 mr-2" />
              Unlink
            </Button>
          </Dialog.Footer>
        )}
      </Dialog.Content>
    </Dialog>
  );
}

function GramOAuthProxyModal({
  isOpen,
  onClose,
  toolset,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolset: Toolset;
}) {
  const telemetry = useTelemetry();
  const queryClient = useQueryClient();

  const addOAuthProxyMutation = useAddOAuthProxyServerMutation({
    onSuccess: () => {
      invalidateAllToolset(queryClient);
      toast.success("Gram OAuth configured successfully");
      telemetry.capture("mcp_event", {
        action: "gram_oauth_proxy_configured",
        slug: toolset.slug,
      });
      onClose();
    },
    onError: (error) => {
      console.error("Failed to configure Gram OAuth:", error);
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to configure Gram OAuth",
      );
    },
  });

  const handleSubmit = () => {
    addOAuthProxyMutation.mutate({
      request: {
        slug: toolset.slug,
        addOAuthProxyServerRequestBody: {
          oauthProxyServer: {
            providerType: "gram",
            slug: "gram-oauth-proxy",
          },
        },
      },
    });
  };

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-w-2xl max-h-[90vh] overflow-hidden">
        <Dialog.Header>
          <Dialog.Title>Gram OAuth</Dialog.Title>
        </Dialog.Header>

        <div className="space-y-4 overflow-auto max-h-[60vh]">
          <div>
            <Type className="font-medium mb-2">Gram OAuth Configuration</Type>
            <Type small className="mb-4">
              Configure Gram OAuth to let users with access to your organization
              use this MCP server. Users will authenticate using their Gram
              credentials.
            </Type>
          </div>
        </div>

        <Dialog.Footer className="flex justify-end">
          <Button
            onClick={handleSubmit}
            disabled={addOAuthProxyMutation.isPending}
          >
            {addOAuthProxyMutation.isPending
              ? "Enabling..."
              : "Enable Gram OAuth"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

function ConnectOAuthModal({
  isOpen,
  onClose,
  toolsetSlug,
  toolset,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolsetSlug: string;
  toolset: Toolset;
}) {
  const session = useSession();
  const queryClient = useQueryClient();
  const isAccountUpgrade = session.gramAccountType === "free";

  // For free accounts, show the FeatureRequestModal
  if (isAccountUpgrade) {
    return (
      <FeatureRequestModal
        isOpen={isOpen}
        onClose={onClose}
        title="Connect OAuth"
        description="A Managed OAuth integration requires upgrading to a pro account type. Someone should be in touch shortly, or feel free to book a meeting directly."
        actionType="mcp_oauth_integration"
        icon={Globe}
        telemetryData={{ slug: toolsetSlug }}
        accountUpgrade={isAccountUpgrade}
      />
    );
  }

  // For non-free accounts, show the tab modal
  return (
    <OAuthTabModal
      isOpen={isOpen}
      onClose={onClose}
      toolsetSlug={toolsetSlug}
      toolset={toolset}
      onSuccess={() => {
        invalidateAllToolset(queryClient);
        toast.success("External OAuth server configured successfully");
        onClose();
      }}
    />
  );
}

function OAuthTabModal({
  isOpen,
  onClose,
  toolsetSlug,
  toolset,
  onSuccess,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolsetSlug: string;
  toolset: Toolset;
  onSuccess: () => void;
}) {
  const [activeTab, setActiveTab] = useState("external");
  const [externalSlug, setExternalSlug] = useState("");
  const [metadataJson, setMetadataJson] = useState("");
  const [jsonError, setJsonError] = useState<string | null>(null);
  const telemetry = useTelemetry();

  // OAuth Proxy form state
  const [proxySlug, setProxySlug] = useState("");
  const [proxyAuthorizationEndpoint, setProxyAuthorizationEndpoint] =
    useState("");
  const [proxyTokenEndpoint, setProxyTokenEndpoint] = useState("");
  const [proxyScopes, setProxyScopes] = useState("");
  const [proxyTokenAuthMethod, setProxyTokenAuthMethod] =
    useState("client_secret_post");
  const [proxyEnvironmentSlug, setProxyEnvironmentSlug] = useState(
    toolset.defaultEnvironmentSlug ?? "",
  );
  const [proxyError, setProxyError] = useState<string | null>(null);

  const hasMultipleOAuth2AuthCode =
    toolset.oauthEnablementMetadata?.oauth2SecurityCount > 1;
  const queryClient = useQueryClient();

  const addExternalOAuthMutation = useAddExternalOAuthServerMutation({
    onSuccess: () => {
      invalidateAllToolset(queryClient);

      telemetry.capture("mcp_event", {
        action: "external_oauth_configured",
        slug: toolsetSlug,
      });

      onSuccess();
    },
    onError: (error) => {
      console.error("Failed to configure external OAuth:", error);
      toast.error(
        error instanceof Error ? error.message : "Failed to configure OAuth",
      );
    },
  });

  const addOAuthProxyMutation = useAddOAuthProxyServerMutation({
    onSuccess: () => {
      invalidateAllToolset(queryClient);

      telemetry.capture("mcp_event", {
        action: "oauth_proxy_configured",
        slug: toolsetSlug,
      });

      onSuccess();
    },
    onError: (error) => {
      console.error("Failed to configure OAuth proxy:", error);
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to configure OAuth proxy",
      );
    },
  });

  const handleExternalSubmit = () => {
    // Validate JSON
    let parsedMetadata;
    try {
      parsedMetadata = JSON.parse(metadataJson);
    } catch (_e) {
      setJsonError("Invalid JSON format");
      return;
    }

    if (!externalSlug.trim()) {
      toast.error("Please provide a slug for the OAuth server");
      return;
    }

    // Validate required OAuth endpoints
    const requiredEndpoints = [
      "authorization_endpoint",
      "token_endpoint",
      "registration_endpoint",
    ];
    const missingEndpoints = requiredEndpoints.filter(
      (endpoint) => !parsedMetadata[endpoint],
    );

    if (missingEndpoints.length > 0) {
      setJsonError(
        `Missing required endpoints: ${missingEndpoints.join(", ")}`,
      );
      return;
    }

    setJsonError(null);
    addExternalOAuthMutation.mutate({
      request: {
        slug: toolsetSlug,
        addExternalOAuthServerRequestBody: {
          externalOauthServer: {
            slug: externalSlug,
            metadata: parsedMetadata,
          },
        },
      },
    });
  };

  const handleProxySubmit = () => {
    setProxyError(null);

    if (!proxySlug.trim()) {
      setProxyError("Please provide a slug for the OAuth proxy server");
      return;
    }

    if (!proxyAuthorizationEndpoint.trim()) {
      setProxyError("Authorization endpoint is required");
      return;
    }

    if (!proxyTokenEndpoint.trim()) {
      setProxyError("Token endpoint is required");
      return;
    }

    if (!proxyEnvironmentSlug.trim()) {
      setProxyError("Environment slug is required");
      return;
    }

    const scopesArray = proxyScopes
      .split(",")
      .map((s) => s.trim())
      .filter((s) => s.length > 0);

    if (scopesArray.length === 0) {
      setProxyError("At least one scope is required");
      return;
    }

    addOAuthProxyMutation.mutate({
      request: {
        slug: toolsetSlug,
        addOAuthProxyServerRequestBody: {
          oauthProxyServer: {
            providerType: "custom",
            slug: proxySlug,
            authorizationEndpoint: proxyAuthorizationEndpoint,
            tokenEndpoint: proxyTokenEndpoint,
            scopesSupported: scopesArray,
            tokenEndpointAuthMethodsSupported: [proxyTokenAuthMethod],
            environmentSlug: proxyEnvironmentSlug,
          },
        },
      },
    });
  };

  return (
    <>
      <Dialog open={isOpen} onOpenChange={onClose}>
        <Dialog.Content className="max-w-6xl max-h-[90vh] overflow-hidden">
          <Dialog.Header>
            <Dialog.Title>Connect OAuth</Dialog.Title>
          </Dialog.Header>

          <Tabs
            value={activeTab}
            onValueChange={setActiveTab}
            className="flex-1"
          >
            <TabsList>
              <TabsTrigger value="external">External Server</TabsTrigger>
              <TabsTrigger value="proxy">OAuth Proxy</TabsTrigger>
            </TabsList>

            <TabsContent
              value="external"
              className="space-y-4 overflow-auto max-h-[60vh]"
            >
              {hasMultipleOAuth2AuthCode && (
                <div className="bg-red-50 border border-red-200 rounded-md p-4 mb-4">
                  <Type small className="text-red-600 mt-1">
                    Not Supported: This MCP server has{" "}
                    {toolset.oauthEnablementMetadata?.oauth2SecurityCount}{" "}
                    OAuth2 security schemes detected.
                  </Type>
                </div>
              )}
              <div>
                <Type className="font-medium mb-2">
                  External OAuth Server Configuration
                </Type>
                <Type muted small className="mb-4">
                  Configure your MCP server to use an external authorization
                  server if your API fits the very specific MCP OAuth
                  requirements.{" "}
                  <Link
                    external
                    to="https://docs.getgram.ai/host-mcp/adding-oauth#authorization-code"
                  >
                    Docs
                  </Link>
                </Type>

                <Stack gap={4}>
                  <div>
                    <Type className="font-medium mb-2">OAuth Server Slug</Type>
                    <Input
                      placeholder="my-oauth-server"
                      value={externalSlug}
                      onChange={setExternalSlug}
                      maxLength={40}
                    />
                  </div>

                  <div>
                    <Type className="font-medium mb-2">
                      OAuth Authorization Server Metadata
                    </Type>
                    {jsonError && (
                      <Type className="!text-red-500 text-sm mt-1">
                        {jsonError}
                      </Type>
                    )}
                    <TextArea
                      placeholder={`{
  "issuer": "https://your-oauth-server.com",
  "authorization_endpoint": "https://your-oauth-server.com/oauth/authorize",
  "registration_endpoint": "https://your-oauth-server.com/oauth/register",
  "token_endpoint": "https://your-oauth-server.com/oauth/token",
  "scopes_supported": ["read", "write"],
  "response_types_supported": ["code"],
  "grant_types_supported": ["authorization_code"],
  "token_endpoint_auth_methods_supported": [
    "client_secret_post"
  ],
  "code_challenge_methods_supported": [
    "plain",
    "S256"
  ]
}`}
                      value={metadataJson}
                      onChange={(value: string) => {
                        setMetadataJson(value);
                        setJsonError(null);
                      }}
                      rows={12}
                      className="font-mono text-sm"
                    />
                  </div>
                </Stack>
              </div>
            </TabsContent>

            <TabsContent
              value="proxy"
              className="space-y-4 overflow-auto max-h-[60vh]"
            >
              <div>
                <Type className="font-medium mb-2">
                  OAuth Proxy Server Configuration
                </Type>
                <Type muted small className="mb-4">
                  ONLY USE FOR INTERNAL SERVERS. Configure an OAuth proxy server
                  to handle OAuth authentication for APIs that don't natively
                  support MCP OAuth requirements. Getting proxy settings correct
                  can be tricky. Need help?{" "}
                  <Link
                    external
                    to="https://calendly.com/d/ctgg-5dv-3kw/intro-to-gram-call"
                  >
                    Book a meeting
                  </Link>
                </Type>

                {proxyError && (
                  <Type className="!text-red-500 text-sm mb-4">
                    {proxyError}
                  </Type>
                )}

                <Stack gap={4}>
                  <div>
                    <Type className="font-medium mb-2">
                      OAuth Proxy Server Slug
                    </Type>
                    <Input
                      placeholder="my-oauth-proxy"
                      value={proxySlug}
                      onChange={setProxySlug}
                      maxLength={40}
                    />
                  </div>

                  <div>
                    <Type className="font-medium mb-2">
                      Authorization Endpoint
                    </Type>
                    <Input
                      placeholder="https://provider.com/oauth/authorize"
                      value={proxyAuthorizationEndpoint}
                      onChange={setProxyAuthorizationEndpoint}
                    />
                  </div>

                  <div>
                    <Type className="font-medium mb-2">Token Endpoint</Type>
                    <Input
                      placeholder="https://provider.com/oauth/token"
                      value={proxyTokenEndpoint}
                      onChange={setProxyTokenEndpoint}
                    />
                  </div>

                  <div>
                    <Type className="font-medium mb-2">
                      Scopes (comma-separated)
                    </Type>
                    <Input
                      placeholder="read, write, openid"
                      value={proxyScopes}
                      onChange={setProxyScopes}
                    />
                  </div>

                  <div>
                    <Type className="font-medium mb-2">
                      Token Endpoint Auth Method
                    </Type>
                    <select
                      className="w-full border rounded px-3 py-2 bg-background"
                      value={proxyTokenAuthMethod}
                      onChange={(e) => setProxyTokenAuthMethod(e.target.value)}
                    >
                      <option value="client_secret_post">
                        client_secret_post
                      </option>
                      <option value="client_secret_basic">
                        client_secret_basic
                      </option>
                      <option value="none">none</option>
                    </select>
                  </div>

                  <div>
                    <Type className="font-medium mb-2">Environment</Type>
                    <EnvironmentDropdown
                      selectedEnvironment={proxyEnvironmentSlug}
                      setSelectedEnvironment={setProxyEnvironmentSlug}
                      tooltip="Select environment for OAuth credentials"
                      className="w-full max-w-full"
                    />
                    <Type muted small className="mt-1">
                      The environment where OAuth client credentials will be
                      stored.
                    </Type>
                  </div>
                </Stack>
              </div>
            </TabsContent>
          </Tabs>

          <Dialog.Footer className="flex justify-end">
            {activeTab === "external" && (
              <Button
                onClick={handleExternalSubmit}
                disabled={
                  hasMultipleOAuth2AuthCode ||
                  addExternalOAuthMutation.isPending ||
                  !externalSlug.trim() ||
                  !metadataJson.trim()
                }
              >
                {addExternalOAuthMutation.isPending
                  ? "Configuring..."
                  : "Configure External OAuth"}
              </Button>
            )}
            {activeTab === "proxy" && (
              <Button
                onClick={handleProxySubmit}
                disabled={
                  addOAuthProxyMutation.isPending ||
                  !proxySlug.trim() ||
                  !proxyAuthorizationEndpoint.trim() ||
                  !proxyTokenEndpoint.trim() ||
                  !proxyEnvironmentSlug.trim()
                }
              >
                {addOAuthProxyMutation.isPending
                  ? "Configuring..."
                  : "Configure OAuth Proxy"}
              </Button>
            )}
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
