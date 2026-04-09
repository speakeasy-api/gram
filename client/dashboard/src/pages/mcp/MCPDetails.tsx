import { Block, BlockInner } from "@/components/block";
import { CodeBlock } from "@/components/code";
import { DetailHero } from "@/components/detail-hero";
import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import {
  InstallPageConfigForm,
  useMcpMetadataMetadataForm,
  type UseMcpMetadataMetadataFormResult,
} from "@/components/mcp_install_page/config_form";
import { Textarea } from "@/components/moon/textarea";
import { Page } from "@/components/page-layout";
import { ServerEnableDialog } from "@/components/server-enable-dialog";
import { ToolList } from "@/components/tool-list";
import { BigToggle } from "@/components/ui/big-toggle";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Link } from "@/components/ui/link";
import { MultiSelect } from "@/components/ui/multi-select";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
import { TextArea } from "@/components/ui/textarea";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useListTools, useToolset } from "@/hooks/toolTypes";
import { useMissingRequiredEnvVars } from "@/hooks/useMissingEnvironmentVariables";
import { useProductTier } from "@/hooks/useProductTier";
import { useToolsetEnvVars } from "@/hooks/useToolsetEnvVars";
import { useCustomDomain, useMcpUrl } from "@/hooks/useToolsetUrl";
import { isHttpTool, Tool, Toolset, useGroupedTools } from "@/lib/toolTypes";
import { cn, getServerURL } from "@/lib/utils";
import { PromptsTabContent } from "@/pages/toolsets/PromptsTab";
import { ResourcesTabContent } from "@/pages/toolsets/resources/ResourcesTab";
import { ServerTabContent } from "@/pages/toolsets/ServerTab";
import { useRoutes } from "@/routes";
import { Confirm, ToolsetEntry } from "@gram/client/models/components";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import {
  invalidateAllGetPeriodUsage,
  invalidateAllToolset,
  invalidateTemplate,
  useAddExternalOAuthServerMutation,
  useAddOAuthProxyServerMutation,
  useExportMcpMetadataMutation,
  useGetMcpMetadata,
  useLatestDeployment,
  useListEnvironments,
  useRemoveOAuthServerMutation,
  useUpdateToolsetMutation,
} from "@gram/client/react-query";
import { Badge, Button, Grid, Icon, Stack } from "@speakeasy-api/moonshine";
import { useQueries, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  CheckCircleIcon,
  Download,
  Globe,
  LockIcon,
  Play,
  Trash2,
  XCircleIcon,
} from "lucide-react";
import { generateText } from "ai";
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { Outlet, useParams } from "react-router";
import { toast } from "sonner";
import { EnvironmentDropdown } from "../environments/EnvironmentDropdown";
import { useModel } from "../playground/Openrouter";
import { onboardingStepStorageKeys } from "../home/Home";
import { AddToolsDialog } from "../toolsets/AddToolsDialog";
import { ToolsetEmptyState } from "../toolsets/ToolsetEmptyState";
import { MCPAuthenticationTab } from "./MCPEnvironmentSettings";
import { MCPPerformanceTab } from "./MCPPerformanceTab";
import {
  useCollections,
  useAttachServer,
  useDetachServer,
} from "@/pages/collections/hooks";
import { Checkbox } from "@/components/ui/checkbox";
import {
  buildMcpRegistriesServeQuery,
  useGramContext,
} from "@gram/client/react-query";

export function MCPDetailsRoot() {
  return <Outlet />;
}

function MCPLoading() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body fullWidth noPadding>
        {/* Hero skeleton */}
        <div className="relative w-full h-64 bg-muted/30 animate-pulse">
          <div className="absolute bottom-0 left-0 right-0 px-8 py-8 max-w-[1270px] mx-auto w-full">
            <Stack gap={2}>
              <div className="h-8 w-64 bg-muted rounded" />
              <div className="h-4 w-96 bg-muted rounded" />
            </Stack>
          </div>
        </div>

        {/* Tabs skeleton */}
        <div className="border-b">
          <div className="max-w-[1270px] mx-auto px-8">
            <div className="flex gap-6 h-11">
              <div className="h-4 w-20 bg-muted rounded animate-pulse" />
              <div className="h-4 w-16 bg-muted rounded animate-pulse" />
              <div className="h-4 w-20 bg-muted rounded animate-pulse" />
              <div className="h-4 w-28 bg-muted rounded animate-pulse" />
            </div>
          </div>
        </div>

        {/* Content skeleton */}
        <div className="max-w-[1270px] mx-auto px-8 py-8 w-full">
          <Stack gap={6}>
            <div className="space-y-4">
              <div className="h-6 w-48 bg-muted rounded animate-pulse" />
              <div className="h-4 w-full max-w-2xl bg-muted rounded animate-pulse" />
              <div className="h-32 w-full bg-muted rounded animate-pulse" />
            </div>
            <div className="space-y-4">
              <div className="h-6 w-40 bg-muted rounded animate-pulse" />
              <div className="h-4 w-full max-w-2xl bg-muted rounded animate-pulse" />
              <div className="h-24 w-full bg-muted rounded animate-pulse" />
            </div>
          </Stack>
        </div>
      </Page.Body>
    </Page>
  );
}

export function MCPDetailPage() {
  const { toolsetSlug } = useParams();
  const routes = useRoutes();

  const { data: toolset, isLoading } = useToolset(toolsetSlug);

  // Call hooks before any conditional returns
  const { url: mcpUrl } = useMcpUrl(toolset);
  const { data: environmentsData } = useListEnvironments();
  const environments = environmentsData?.environments ?? [];

  // Fetch MCP metadata early to use in useMissingRequiredEnvVars
  const { data: mcpMetadataData } = useGetMcpMetadata(
    { toolsetSlug: toolset?.slug || "" },
    undefined,
    { enabled: !!toolset?.slug, throwOnError: false },
  );
  const mcpMetadataForBadge = mcpMetadataData?.metadata;

  // Tab state controlled by URL hash - initialize directly from hash
  const [activeTab, setActiveTab] = useState<string>(() => {
    const hash = window.location.hash.slice(1); // Remove the '#'
    const validTabs = [
      "overview",
      "tools",
      "resources",
      "prompts",
      "authentication",
      "performance",
      "settings",
    ];
    return hash && validTabs.includes(hash) ? hash : "overview";
  });

  const handleTabChange = (value: string) => {
    setActiveTab(value);
    const url = new URL(window.location.href);
    url.hash = value;
    window.history.replaceState(null, "", url.toString());
  };

  useEffect(() => {
    localStorage.setItem(onboardingStepStorageKeys.configure, "true");
  }, []);

  // Calculate if there are missing required env vars for the tab indicator
  // Must be before early return to avoid hooks order issues
  const missingRequiredEnvVars = useMissingRequiredEnvVars(
    toolset,
    environments,
    toolset?.defaultEnvironmentSlug || "default",
    mcpMetadataForBadge,
  );

  if (isLoading || !toolset) {
    return <MCPLoading />;
  }

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
      <Page.Body fullWidth noPadding className="gap-0">
        <DetailHero
          actions={
            <>
              <routes.playground.Link queryParams={{ toolset: toolset.slug }}>
                <Button
                  variant="secondary"
                  size="md"
                  className="bg-background hover:bg-accent border-border"
                >
                  <Button.LeftIcon>
                    <Play className="w-4 h-4" />
                  </Button.LeftIcon>
                  <Button.Text>Playground</Button.Text>
                </Button>
              </routes.playground.Link>
              <MCPEnableButton toolset={toolset} />
            </>
          }
        >
          <div className="flex items-end justify-between">
            <Stack gap={2}>
              <div className="flex items-center gap-3 ml-1">
                <Heading variant="h1">{toolset.name}</Heading>
                {statusBadge}
              </div>
              <div className="flex items-center gap-2 ml-1">
                <Type className="max-w-2xl truncate text-muted-foreground">
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
                  className="shrink-0 text-muted-foreground hover:text-foreground"
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
        </DetailHero>

        {/* Sub-navigation tabs */}
        <Tabs
          value={activeTab}
          onValueChange={handleTabChange}
          className="w-full flex-1 flex flex-col"
        >
          <div className="border-b">
            <div className="max-w-[1270px] mx-auto px-8">
              <TabsList className="h-auto bg-transparent p-0 gap-6 rounded-none">
                <PageTabsTrigger value="overview">Overview</PageTabsTrigger>
                <PageTabsTrigger value="tools">Tools</PageTabsTrigger>
                <PageTabsTrigger value="resources">Resources</PageTabsTrigger>
                <PageTabsTrigger value="prompts">Prompts</PageTabsTrigger>
                <PageTabsTrigger value="authentication">
                  <span className="flex items-center gap-1.5">
                    Authentication
                    {missingRequiredEnvVars > 0 && (
                      <AlertTriangle className="h-3.5 w-3.5 text-warning" />
                    )}
                  </span>
                </PageTabsTrigger>
                <PageTabsTrigger value="performance">
                  Performance
                </PageTabsTrigger>
                <PageTabsTrigger value="settings">Settings</PageTabsTrigger>
              </TabsList>
            </div>
          </div>

          {/* Tab Content */}
          <div className="max-w-[1270px] mx-auto px-8 py-8 w-full">
            <TabsContent value="overview" className="mt-0 w-full">
              <MCPOverviewTab toolset={toolset} onTabChange={handleTabChange} />
            </TabsContent>

            <TabsContent value="tools" className="mt-0 w-full">
              <MCPToolsTab toolset={toolset} />
            </TabsContent>

            <TabsContent value="resources" className="mt-0 w-full">
              <MCPResourcesTab toolset={toolset} />
            </TabsContent>

            <TabsContent value="prompts" className="mt-0 w-full">
              <MCPPromptsTab toolset={toolset} />
            </TabsContent>

            <TabsContent value="authentication" className="mt-0 w-full">
              <MCPAuthenticationTab toolset={toolset} />
            </TabsContent>

            <TabsContent value="performance" className="mt-0 w-full">
              <MCPPerformanceTab toolset={toolset} />
            </TabsContent>

            <TabsContent value="settings" className="mt-0 w-full">
              <MCPSettingsTab toolset={toolset} />
            </TabsContent>
          </div>
        </Tabs>
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
        variant="primary"
        onClick={() => setIsServerEnableDialogOpen(true)}
      >
        {toolset.mcpEnabled ? "DISABLE" : "ENABLE"}
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
function MCPOverviewTab({
  toolset,
  onTabChange,
}: {
  toolset: Toolset;
  onTabChange: (tab: string) => void;
}) {
  const { url: mcpUrl } = useMcpUrl(toolset);

  const result = useGetMcpMetadata({ toolsetSlug: toolset.slug }, undefined, {
    retry: (_, err) => {
      if (err instanceof GramError && err.statusCode === 404) {
        return false;
      }
      return true;
    },
    throwOnError: false,
  });

  const form = useMcpMetadataMetadataForm(toolset.slug, result.data?.metadata);
  const isLoading = result.isLoading || form.isLoading;

  return (
    <Stack className="mb-4">
      <PageSection
        heading="Hosted URL"
        description="The URL you or your users will use to access this MCP server."
      >
        <CodeBlock className="mb-2">{mcpUrl ?? ""}</CodeBlock>
      </PageSection>

      <PageSection
        heading="Install Page"
        description="Share this page with your users to give simple instructions for getting started with your MCP in their client like Cursor or Claude Desktop."
      >
        {!toolset.mcpIsPublic && (
          <Type small italic destructive>
            Your server is private. To share with external users, you must make
            it public in the{" "}
            <button
              className="underline appearance-none"
              onClick={() => onTabChange("settings")}
            >
              server settings
            </button>
            .
          </Type>
        )}
        <Stack className="mt-2" gap={1}>
          <InstallPageConfigForm
            toolset={toolset}
            form={form}
            isLoading={isLoading}
          />
        </Stack>
      </PageSection>

      <PageSection
        heading="Server Instructions"
        description="Instructions returned to LLMs when they connect to your MCP server. Describe how your tools work together, required workflows, and any constraints."
      >
        <ServerInstructionsSection
          toolset={toolset}
          form={form}
          isLoading={isLoading}
        />
      </PageSection>
    </Stack>
  );
}

/**
 * Server Instructions Section - textarea + generate + save
 */
const INSTRUCTIONS_SOFT_LIMIT = 2000;

function ServerInstructionsSection({
  toolset,
  form,
  isLoading,
}: {
  toolset: Toolset;
  form: UseMcpMetadataMetadataFormResult;
  isLoading: boolean;
}) {
  const charCount = form.instructionsHandlers.value?.length ?? 0;
  const overLimit = charCount > INSTRUCTIONS_SOFT_LIMIT;

  return (
    <Stack gap={3}>
      <div className="relative">
        <Textarea
          placeholder={`Describe how your tools work together, required workflows,\nand any constraints (rate limits, auth requirements, etc.).\n\nKeep it concise — don't repeat individual tool descriptions.`}
          className="w-full min-h-[150px]"
          value={form.instructionsHandlers.value ?? ""}
          onChange={form.instructionsHandlers.onChange}
        />
        {charCount > 0 && (
          <span
            className={cn(
              "absolute bottom-2 right-3 text-xs",
              overLimit ? "text-destructive" : "text-muted-foreground",
            )}
          >
            {charCount.toLocaleString()} /{" "}
            {INSTRUCTIONS_SOFT_LIMIT.toLocaleString()}
          </span>
        )}
      </div>
      <Stack direction="horizontal" gap={2} justify="end">
        <GenerateInstructionsButton toolset={toolset} form={form} />
        <Button
          onClick={async () => {
            try {
              await form.saveAsync();
              toast.success("Server instructions saved.");
            } catch {
              toast.error("Failed to save instructions.");
            }
          }}
          disabled={isLoading || !form.instructionsDirty}
          size="sm"
        >
          <Button.Text>Save</Button.Text>
        </Button>
      </Stack>
    </Stack>
  );
}

function GenerateInstructionsButton({
  toolset,
  form,
}: {
  toolset: Toolset;
  form: UseMcpMetadataMetadataFormResult;
}) {
  const [generating, setGenerating] = useState(false);
  const { data: fullToolset } = useToolset(toolset.slug);
  const model = useModel("anthropic/claude-sonnet-4.5");

  const tools = fullToolset?.tools ?? [];

  const handleGenerate = async () => {
    if (tools.length === 0) {
      return;
    }

    setGenerating(true);
    try {
      const res = await generateText({
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        model: model as any,
        prompt: `Write server instructions for the MCP server described below. Server instructions are returned to LLMs when they connect — they serve as a "user manual" independent of individual tool descriptions.

Best practices:
DO: Focus on cross-feature relationships (how tools work together, required sequences), document operational patterns and workflows, be explicit about constraints and limitations, keep it short like a quick-reference card.
DO NOT: Duplicate individual tool descriptions, include marketing claims, try to change model personality, write lengthy prose.

Keep the total output under ${INSTRUCTIONS_SOFT_LIMIT} characters.

Server details:
${JSON.stringify({ name: toolset.name, tools: tools.map((t) => ({ name: t.name, description: t.description })) }, null, 2)}

Respond with ONLY the server instructions as plain text. Do not wrap in JSON or code fences.`,
      });

      // Populate the textarea via a synthetic change event
      const syntheticEvent = {
        target: { value: res.text.trim() },
      } as React.ChangeEvent<HTMLTextAreaElement>;
      form.instructionsHandlers.onChange(syntheticEvent);
    } catch (err) {
      console.error("Failed to generate instructions:", err);
      toast.error("Failed to generate instructions. Please try again.");
    } finally {
      setGenerating(false);
    }
  };

  return (
    <Button
      variant="secondary"
      size="sm"
      onClick={handleGenerate}
      disabled={generating || tools.length === 0}
    >
      <Button.LeftIcon>
        <Icon name="wand-sparkles" className="w-4 h-4" />
      </Button.LeftIcon>
      <Button.Text>
        {generating ? "Generating..." : "Generate with AI"}
      </Button.Text>
    </Button>
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

  // Check if this is an external MCP proxy server
  const isExternalMcpProxy = fullToolset?.kind === "external-mcp-proxy";

  // Check if we have orphaned tool URNs (URNs exist but tools were deleted)
  const hasOrphanedTools =
    (fullToolset?.toolUrns?.length ?? 0) > 0 &&
    fullToolset?.rawTools.length === 0;

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
    [fullToolset?.toolUrns, toolset.slug, updateToolsetMutation, telemetry],
  );

  const handleTestInPlayground = useCallback(() => {
    routes.playground.goTo(toolset.slug);
  }, [toolset.slug, routes.playground]);

  // Group filtering
  const grouped = useGroupedTools(tools);
  const [selectedGroups, setSelectedGroups] = useState<string[]>(
    grouped.map((group) => group.key),
  );

  const groupKeys = grouped.map((group) => group.key);
  // Set initial selected groups when the tool list resolves
  useEffect(() => {
    setSelectedGroups(groupKeys);
  }, [groupKeys.join(",")]);

  const handleToolUpdate = async (
    tool: Tool,
    updates: {
      name?: string;
      description?: string;
      title?: string;
      readOnlyHint?: boolean;
      destructiveHint?: boolean;
      idempotentHint?: boolean;
      openWorldHint?: boolean;
    },
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

    toast.success("Tool updated");
    refetch();
  };

  // For external MCP proxy servers, show the server info instead of tools list
  if (isExternalMcpProxy && fullToolset) {
    return <ServerTabContent toolset={fullToolset} />;
  }

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
      {!isExternalMcpProxy && (
        <Stack
          direction="horizontal"
          justify="space-between"
          align="center"
          className="mb-4"
        >
          <Heading variant="h3">Tools</Heading>
          <Stack direction="horizontal" gap={2}>
            <routes.customTools.Link>
              <Button variant="secondary" size="sm">
                <Button.Text>Custom Tools</Button.Text>
              </Button>
            </routes.customTools.Link>
            <Button onClick={() => setAddToolsDialogOpen(true)} size="sm">
              <Button.LeftIcon>
                <Icon name="plus" className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Add Tools</Button.Text>
            </Button>
          </Stack>
        </Stack>
      )}

      {/* Group filter */}
      {!isExternalMcpProxy && groupFilterItems.length > 1 && (
        <MultiSelect
          options={groupFilterItems}
          selectedValues={selectedGroups}
          setSelectedValues={setSelectedGroups}
          placeholder="Filter tools"
          className="w-fit mb-4 capitalize"
        />
      )}

      {/* Tools list or empty state */}
      {hasOrphanedTools ? (
        <Stack gap={4} align="center" className="py-12">
          <div className="text-center max-w-md">
            <AlertTriangle className="w-12 h-12 mx-auto mb-4 text-warning" />
            <Heading variant="h3" className="mb-2">
              Tool Source Deleted
            </Heading>
            <Type muted>
              This MCP server has tool references, but the underlying source has
              been deleted. Re-adding the source will reinstate the tools.
            </Type>
          </div>
        </Stack>
      ) : toolsToDisplay.length > 0 ? (
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
      {fullToolset && !isExternalMcpProxy && (
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
 * Resources Tab - Wraps the existing ResourcesTabContent for MCP page
 */
function MCPResourcesTab({ toolset }: { toolset: Toolset }) {
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const { data: fullToolset } = useToolset(toolset.slug);

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      telemetry.capture("toolset_event", { action: "toolset_updated" });
      invalidateAllToolset(queryClient);
    },
  });

  if (!fullToolset) return null;

  return (
    <ResourcesTabContent
      toolset={fullToolset}
      updateToolsetMutation={updateToolsetMutation}
    />
  );
}

/**
 * Prompts Tab - Wraps the existing PromptsTabContent for MCP page
 */
function MCPPromptsTab({ toolset }: { toolset: Toolset }) {
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const { data: fullToolset } = useToolset(toolset.slug);

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      telemetry.capture("toolset_event", { action: "toolset_updated" });
      invalidateAllToolset(queryClient);
    },
  });

  if (!fullToolset) return null;

  return (
    <PromptsTabContent
      toolset={fullToolset}
      updateToolsetMutation={updateToolsetMutation}
    />
  );
}

/**
 * Settings Tab - Visibility, Slug, Custom Domain, Actions, Danger Zone
 */
function MCPSettingsTab({ toolset }: { toolset: Toolset }) {
  const telemetry = useTelemetry();
  const queryClient = useQueryClient();
  const productTier = useProductTier();
  const { orgSlug } = useParams();
  const { domain } = useCustomDomain();
  const routes = useRoutes();
  const client = useSdkClient();
  const { data: deploymentResult, refetch: refetchDeployment } =
    useLatestDeployment();
  const deployment = deploymentResult?.deployment;

  // OAuth state
  const isOAuthConnected = !!(
    toolset?.oauthProxyServer || toolset?.externalOauthServer
  );
  const availableOAuthAuthCode =
    toolset?.oauthEnablementMetadata?.oauth2SecurityCount > 0;
  const [isOAuthModalOpen, setIsOAuthModalOpen] = useState(false);
  const [isGramOAuthModalOpen, setIsGramOAuthModalOpen] = useState(false);
  const [isOAuthDetailsModalOpen, setIsOAuthDetailsModalOpen] = useState(false);

  // Delete mcp server state
  const [isDeleteDialogOpen, setIsDeleteDialogOpen] = useState(false);
  const [isDeletingMcpServer, setIsDeletingMcpServer] = useState(false);

  // Export mutation
  const exportMutation = useExportMcpMetadataMutation();

  const handleExportJson = async () => {
    if (!toolset?.mcpSlug) {
      toast.error("MCP server slug not available");
      return;
    }

    const toastId = toast.loading("Exporting MCP configuration...");

    try {
      const result = await exportMutation.mutateAsync({
        request: {
          exportMcpMetadataRequestBody: {
            mcpSlug: toolset.mcpSlug,
          },
        },
      });

      const blob = new Blob([JSON.stringify(result, null, 2)], {
        type: "application/json",
      });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${toolset.mcpSlug}-mcp-config.json`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);

      telemetry.capture("mcp_event", {
        action: "mcp_json_exported",
        slug: toolset.slug,
      });

      toast.success("MCP configuration exported", { id: toastId });
    } catch (error) {
      console.error("Failed to export MCP configuration:", error);
      toast.error(
        `Failed to export: ${error instanceof Error ? error.message : "Unknown error"}`,
        {
          id: toastId,
        },
      );
    }
  };

  const handleDeleteMcpServer = async () => {
    if (!toolset) return;

    setIsDeletingMcpServer(true);

    try {
      const externalMcpUrn = toolset.toolUrns?.find((urn) =>
        urn.includes(":externalmcp:"),
      );

      if (externalMcpUrn && deployment) {
        const parts = externalMcpUrn.split(":");
        const externalMcpSlug = parts[2];

        if (externalMcpSlug) {
          await client.deployments.evolveDeployment({
            evolveForm: {
              deploymentId: deployment.id,
              nonBlocking: true,
              excludeExternalMcps: [externalMcpSlug],
            },
          });
        }
      }

      await client.toolsets.deleteBySlug({ slug: toolset.slug });

      telemetry.capture("mcp_event", {
        action: "mcp_server_deleted",
        slug: toolset.slug,
      });

      invalidateAllToolset(queryClient);
      invalidateAllGetPeriodUsage(queryClient);
      refetchDeployment();

      toast.success(`MCP server "${toolset.slug}" deleted`);
      setIsDeleteDialogOpen(false);
      routes.mcp.goTo();
    } catch (error) {
      console.error("Failed to delete MCP server:", error);
      toast.error(`Failed to delete MCP server "${toolset.slug}"`);
    } finally {
      setIsDeletingMcpServer(false);
    }
  };

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

  // Account for legacy Pro tier which can still access custom domains
  const canAccessCustomDomain = !productTier.includes("base");

  const customDomain =
    domain && canAccessCustomDomain && !toolset.customDomainId ? (
      linkDomainButton
    ) : (
      <Button
        variant="secondary"
        size="sm"
        onClick={() => {
          if (!canAccessCustomDomain) {
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
              "Only users with a Gram API Key from this project can read the tools hosted by this server.",
          },
        ]}
        selectedValue={isPublic ? "public" : "private"}
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

      {/* Publishing - only show for non-catalog toolsets */}
      {!toolset.toolUrns?.some((u) => u.startsWith("tools:externalmcp:")) && (
        <MCPPublishingSection toolset={toolset} />
      )}

      <PageSection
        heading="Actions"
        description="Export or configure your MCP server."
      >
        <Stack direction="horizontal" gap={3}>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="secondary"
                size="md"
                onClick={handleExportJson}
                disabled={!toolset?.mcpEnabled || !toolset?.mcpSlug}
              >
                <Button.LeftIcon>
                  <Download className="w-4 h-4" />
                </Button.LeftIcon>
                <Button.Text>Export JSON</Button.Text>
              </Button>
            </TooltipTrigger>
            {!toolset?.mcpEnabled && (
              <TooltipContent>
                Enable server to export configuration
              </TooltipContent>
            )}
          </Tooltip>
          <Tooltip>
            <TooltipTrigger asChild>
              {!toolset?.mcpEnabled ||
              (toolset.mcpIsPublic && !availableOAuthAuthCode) ? (
                <span className="inline-block">
                  <Button variant="secondary" size="md" disabled={true}>
                    <Button.Text>
                      {isOAuthConnected ? "OAuth Connected" : "Configure OAuth"}
                    </Button.Text>
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
                  <Button.Text>
                    {isOAuthConnected ? "OAuth Connected" : "Configure OAuth"}
                  </Button.Text>
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
        </Stack>
      </PageSection>

      {/* Danger Zone */}
      <div className="border border-destructive/30 rounded-lg p-6 mt-8">
        <Type variant="subheading" className="text-destructive mb-1">
          Danger Zone
        </Type>
        <Type muted small className="mb-4">
          Permanently delete this MCP server. This action cannot be undone.
        </Type>
        <Button
          variant="destructive-primary"
          size="md"
          onClick={() => setIsDeleteDialogOpen(true)}
          disabled={isDeleteDialogOpen}
        >
          <Button.LeftIcon>
            <Trash2 className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Delete MCP Server</Button.Text>
        </Button>
      </div>

      <Dialog
        open={isDeleteDialogOpen}
        onOpenChange={(open) => {
          if (!isDeletingMcpServer) setIsDeleteDialogOpen(open);
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Delete MCP Server</Dialog.Title>
          </Dialog.Header>
          <div className="space-y-4 py-4">
            <Type variant="body">
              <code className="font-mono font-bold px-1 py-0.5 bg-muted rounded">
                {toolset.name}
              </code>{" "}
              and all its configuration will be permanently deleted. Connected
              clients will immediately lose access. This action cannot be
              undone.
            </Type>
            <div className="flex justify-end space-x-2">
              <Button
                variant="secondary"
                onClick={() => setIsDeleteDialogOpen(false)}
                disabled={isDeletingMcpServer}
              >
                Cancel
              </Button>
              <Button
                variant="destructive-primary"
                onClick={handleDeleteMcpServer}
                disabled={isDeletingMcpServer}
              >
                Delete MCP Server
              </Button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog>

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
        title="MCP Server Limit Reached"
        description={`You have reached the maximum number of MCP servers for the Base plan. Someone should be in touch shortly, or feel free to book a meeting directly to upgrade.`}
        actionType="max_public_mcp_servers"
        icon={Globe}
        telemetryData={{ slug: toolset.slug }}
        accountUpgrade
      />
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
    </Stack>
  );
}

// Keep the old MCPDetails for backward compatibility (can be removed later)
export function MCPDetails({ toolset }: { toolset: Toolset }) {
  return <MCPSettingsTab toolset={toolset} />;
}

function MCPPublishingSection({ toolset }: { toolset: Toolset }) {
  const gramClient = useGramContext();
  const { data: collections, isLoading: collectionsLoading } = useCollections();
  const attachServer = useAttachServer();
  const detachServer = useDetachServer();
  const [selectedIds, setSelectedIds] = useState<Set<string> | null>(null);
  const [isSaving, setIsSaving] = useState(false);

  // Fetch servers for each collection to determine which ones this toolset is in
  const serveQueries = useQueries({
    queries: collections.map((c) => ({
      ...buildMcpRegistriesServeQuery(gramClient, { collectionSlug: c.slug! }),
      enabled: !!c.slug,
    })),
  });

  // Build a set of collection IDs that this toolset is published to
  const publishedCollectionIds = useMemo(() => {
    const ids = new Set<string>();
    for (let i = 0; i < collections.length; i++) {
      const servers = serveQueries[i]?.data?.servers ?? [];
      for (const s of servers) {
        const parts = s.registrySpecifier?.split("/") ?? [];
        const slug = parts[parts.length - 1];
        if (slug === toolset.mcpSlug) {
          ids.add(collections[i].id);
          break;
        }
      }
    }
    return ids;
  }, [collections, serveQueries, toolset.mcpSlug]);

  // Initialize local state from server state on first load
  const effectiveSelected = selectedIds ?? publishedCollectionIds;

  const hasChanges = useMemo(() => {
    if (!selectedIds) return false;
    if (selectedIds.size !== publishedCollectionIds.size) return true;
    for (const id of selectedIds) {
      if (!publishedCollectionIds.has(id)) return true;
    }
    return false;
  }, [selectedIds, publishedCollectionIds]);

  const toggleCollection = (collectionId: string) => {
    setSelectedIds((prev) => {
      const current = prev ?? new Set(publishedCollectionIds);
      const next = new Set(current);
      if (next.has(collectionId)) {
        next.delete(collectionId);
      } else {
        next.add(collectionId);
      }
      return next;
    });
  };

  const handleSave = async () => {
    if (!selectedIds) return;
    setIsSaving(true);
    try {
      const toAttach = [...selectedIds].filter(
        (id) => !publishedCollectionIds.has(id),
      );
      const toDetach = [...publishedCollectionIds].filter(
        (id) => !selectedIds.has(id),
      );

      await Promise.all([
        ...toAttach.map((collectionId) =>
          attachServer.mutateAsync({
            request: {
              attachServerRequestBody: { collectionId, toolsetId: toolset.id },
            },
          }),
        ),
        ...toDetach.map((collectionId) =>
          detachServer.mutateAsync({
            request: {
              attachServerRequestBody: { collectionId, toolsetId: toolset.id },
            },
          }),
        ),
      ]);
      setSelectedIds(null);
    } finally {
      setIsSaving(false);
    }
  };

  const handleDiscard = () => {
    setSelectedIds(null);
  };

  const isLoading = collectionsLoading || serveQueries.some((q) => q.isLoading);

  return (
    <PageSection
      heading="Publishing"
      description="Publish this server to collections so it can be discovered and installed by others in your organization."
    >
      <Block label="Collections" className="p-0">
        <BlockInner>
          {isLoading ? (
            <Type muted small>
              Loading collections...
            </Type>
          ) : collections.length === 0 ? (
            <Type muted small>
              No collections available.
            </Type>
          ) : (
            <Stack direction="vertical" gap={2}>
              {collections.map((collection) => (
                <label
                  key={collection.id}
                  className="flex items-center gap-3 cursor-pointer"
                >
                  <Checkbox
                    checked={effectiveSelected.has(collection.id)}
                    disabled={isSaving}
                    onCheckedChange={() => toggleCollection(collection.id)}
                  />
                  <Stack direction="vertical" gap={0}>
                    <Type small className="font-medium">
                      {collection.name}
                    </Type>
                    {collection.description && (
                      <Type muted small>
                        {collection.description}
                      </Type>
                    )}
                  </Stack>
                </label>
              ))}
            </Stack>
          )}
        </BlockInner>
        {hasChanges && (
          <BlockInner>
            <Stack direction="horizontal" gap={2}>
              <Button size="sm" disabled={isSaving} onClick={handleSave}>
                <Button.Text>{isSaving ? "Saving..." : "Save"}</Button.Text>
              </Button>
              <Button
                size="sm"
                variant="secondary"
                disabled={isSaving}
                onClick={handleDiscard}
              >
                <Button.Text>Discard</Button.Text>
              </Button>
            </Stack>
          </BlockInner>
        )}
      </Block>
    </PageSection>
  );
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
    "Gram${toolset.slug.replace(/-/g, "").replace(/^./, (c) => c.toUpperCase())}": {
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
    "Gram${toolset.slug.replace(/-/g, "").replace(/^./, (c) => c.toUpperCase())}": {
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
                  {toolset.oauthProxyServer.audience && (
                    <div>
                      <Type small className="font-medium text-muted-foreground">
                        Audience:
                      </Type>
                      <CodeBlock className="mt-1">
                        {toolset.oauthProxyServer.audience}
                      </CodeBlock>
                    </div>
                  )}
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
                        ? `${new URL(mcpUrl).origin}/.well-known/oauth-authorization-server/mcp/${
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
  const productTier = useProductTier();
  const queryClient = useQueryClient();
  const isAccountUpgrade = productTier.includes("base");

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
  // Extract discovered OAuth metadata from external MCP tools.
  // Uses rawTools because proxy-type tools are filtered out of toolset.tools.
  // Builds metadata matching the format the old server-side fallback produced:
  // issuer = Gram's MCP URL, upstream endpoints passed through, plus standard
  // response_types_supported, grant_types_supported, code_challenge_methods_supported.
  const discoveredOAuth = useMemo(() => {
    const baseURL = getServerURL();
    const mcpSlug = toolset.mcpSlug;
    for (const tool of toolset.rawTools) {
      const def = tool.externalMcpToolDefinition;
      if (!def?.requiresOauth) continue;

      if (!def.oauthAuthorizationEndpoint && !def.oauthTokenEndpoint) continue;

      const metadata: Record<string, unknown> = {
        issuer: `${baseURL}/mcp/${mcpSlug}`,
        response_types_supported: ["code"],
        grant_types_supported: ["authorization_code", "refresh_token"],
        code_challenge_methods_supported: ["S256"],
      };
      if (def.oauthAuthorizationEndpoint)
        metadata.authorization_endpoint = def.oauthAuthorizationEndpoint;
      if (def.oauthTokenEndpoint)
        metadata.token_endpoint = def.oauthTokenEndpoint;
      if (def.oauthRegistrationEndpoint)
        metadata.registration_endpoint = def.oauthRegistrationEndpoint;
      if (def.oauthScopesSupported?.length)
        metadata.scopes_supported = def.oauthScopesSupported;

      return {
        slug: def.slug,
        name: def.registryServerName,
        version: def.oauthVersion,
        metadata,
      };
    }
    return null;
  }, [toolset.rawTools, toolset.mcpSlug]);

  const [activeTab, setActiveTab] = useState("external");
  const [externalSlug, setExternalSlug] = useState("");
  const [metadataJson, setMetadataJson] = useState("");
  const [jsonError, setJsonError] = useState<string | null>(null);
  const [prefilled, setPrefilled] = useState<Record<string, boolean>>({});
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
  const [proxyAudience, setProxyAudience] = useState("");
  const [proxyError, setProxyError] = useState<string | null>(null);

  const applyDiscoveredOAuth = useCallback(
    (tab: "external" | "proxy") => {
      if (!discoveredOAuth) return;
      if (tab === "external") {
        setExternalSlug(discoveredOAuth.slug);
        setMetadataJson(JSON.stringify(discoveredOAuth.metadata, null, 2));
        setJsonError(null);
      } else {
        setProxySlug(discoveredOAuth.slug);
        const m = discoveredOAuth.metadata;
        if (typeof m.authorization_endpoint === "string")
          setProxyAuthorizationEndpoint(m.authorization_endpoint);
        if (typeof m.token_endpoint === "string")
          setProxyTokenEndpoint(m.token_endpoint);
        if (Array.isArray(m.scopes_supported))
          setProxyScopes(m.scopes_supported.join(", "));
      }
      setPrefilled((prev) => ({ ...prev, [tab]: true }));
    },
    [discoveredOAuth],
  );

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
    } catch {
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
            audience: proxyAudience || undefined,
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
              {discoveredOAuth && !prefilled.external && (
                <div className="border border-border bg-muted/50 rounded-md p-4 mb-4 flex items-start justify-between gap-4">
                  <div>
                    <Type small className="font-medium">
                      OAuth detected from {discoveredOAuth.name}
                    </Type>
                    <Type muted small className="mt-1">
                      We discovered OAuth {discoveredOAuth.version} metadata
                      from this server. You can use it to pre-fill the form
                      below.
                    </Type>
                  </div>
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => applyDiscoveredOAuth("external")}
                  >
                    Apply
                  </Button>
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
                      <Type className="text-red-500! text-sm mt-1">
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

                {discoveredOAuth && !prefilled.proxy && (
                  <div className="border border-border bg-muted/50 rounded-md p-4 mb-4 flex items-start justify-between gap-4">
                    <div>
                      <Type small className="font-medium">
                        OAuth detected from {discoveredOAuth.name}
                      </Type>
                      <Type muted small className="mt-1">
                        We discovered OAuth {discoveredOAuth.version} metadata
                        from this server. You can use it to pre-fill the
                        endpoints below.
                      </Type>
                    </div>
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => applyDiscoveredOAuth("proxy")}
                    >
                      Apply
                    </Button>
                  </div>
                )}

                {proxyError && (
                  <Type className="text-red-500! text-sm mb-4">
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
                      Audience (optional)
                    </Type>
                    <Input
                      placeholder="https://api.example.com"
                      value={proxyAudience}
                      onChange={setProxyAudience}
                    />
                    <Type muted small className="mt-1">
                      The audience parameter sent to the upstream OAuth
                      provider. Required by some providers (e.g. Auth0) to
                      return JWT access tokens.
                    </Type>
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
