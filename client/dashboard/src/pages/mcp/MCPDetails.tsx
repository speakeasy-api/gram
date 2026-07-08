import { Block, BlockInner } from "@/components/block";
import { CodeBlock } from "@/components/code";
import { MCPPublishingSection as SharedMCPPublishingSection } from "./MCPPublishingSection";
import { MCPToolFilteringSection } from "@/components/mcp-tool-filtering-section";
import {
  useMcpMetadataMetadataForm,
  type UseMcpMetadataMetadataFormResult,
} from "@/components/mcp_install_page/useMcpMetadataForm";
import { Textarea } from "@/components/moon/textarea";
import { Page } from "@/components/page-layout";
import { PublicMcpWarningDialog } from "@/components/public-mcp-warning-dialog";
import { ServerEnableDialog } from "@/components/server-enable-dialog";
import {
  RouteNotFoundState,
  SecondaryRouteAction,
} from "@/components/route-not-found-state";
import { ToolList } from "@/components/tool-list";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { MultiSelect } from "@/components/ui/multi-select";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { RequireScope } from "@/components/require-scope";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRBAC } from "@/hooks/useRBAC";
import { useToolset } from "@/hooks/toolTypes";
import { useToolUpdate } from "@/hooks/useToolUpdate";
import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { useProductTier } from "@/hooks/useProductTier";
import { useCustomDomain, useMcpUrl } from "@/hooks/useToolsetUrl";
import { DEFAULT_MODEL } from "@/lib/models";
import { isNotFoundError } from "@/lib/route-errors";
import { Toolset, useGroupedTools } from "@/lib/toolTypes";
import { cn, getServerURL } from "@/lib/utils";
import { PromptsTabContent } from "@/pages/toolsets/PromptsTab";
import { ResourcesTabContent } from "@/pages/toolsets/resources/ResourcesTab";
import { ServerTabContent } from "@/pages/toolsets/ServerTab";
import {
  EXCLUDED_TAG_KEY,
  MCPToolFilterScopesPanel,
} from "@/pages/mcp/MCPToolFilterScopesPanel";
import { useRoutes } from "@/routes";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useAddOAuthProxyServerMutation } from "@gram/client/react-query/addOAuthProxyServer.js";
import { useExportMcpMetadataMutation } from "@gram/client/react-query/exportMcpMetadata.js";
import { useGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { invalidateAllGetPeriodUsage } from "@gram/client/react-query/getPeriodUsage.js";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import {
  invalidateListToolsetToolFilters,
  useListToolsetToolFilters,
} from "@gram/client/react-query/listToolsetToolFilters.js";
import { invalidateAllListToolsets } from "@gram/client/react-query/listToolsets.js";
import { useRemoveOAuthServerMutation } from "@gram/client/react-query/removeOAuthServer.js";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { useUpdateToolsetMutation } from "@gram/client/react-query/updateToolset.js";
import {
  Badge,
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
  Stack,
} from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  Check,
  ChevronDown,
  Download,
  Globe,
  Pencil,
  Trash2,
} from "lucide-react";
import { generateText } from "ai";
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { Navigate, useLocation, useParams } from "react-router";
import { toast } from "sonner";
import { useModel } from "../playground/Openrouter";
import { AddToolsDialog } from "../toolsets/AddToolsDialog";
import { ToolsetEmptyState } from "../toolsets/ToolsetEmptyState";
import { useToolsets } from "../toolsets/useToolsets";
import { getSystemProvidedVariables } from "./environmentVariableUtils";
import { useMcpSlugValidation } from "./mcp-details-utils";
import { MCPAuthenticationTab } from "./MCPEnvironmentSettings";
import {
  activeTabFromPath,
  initialTabFromHash,
  mcpDetailTabHref,
  MCP_DETAIL_TAB_URLS,
  type TabValue,
} from "./MCPDetailsRouting";
import { MCPOverviewTab } from "./overview/MCPOverviewTab";
import { MCPPerformanceTab } from "./MCPPerformanceTab";
import { MCPTeamAccessTab } from "./MCPTeamAccessTab";
import { useEnvironmentVariables } from "./useEnvironmentVariables";

// Mirrors the sidenav'd shell (no hero, no top tab strip) and roughly the
// shape of the Overview tab — the default landing tab — since exact content
// varies per sub-page and this is only visible for a brief loading flash.
function MCPLoading() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body fullWidth className="gap-0">
        <div className="mx-auto w-full max-w-[1270px] flex-1">
          <Stack gap={6} className="mb-4">
            <div className="bg-muted/30 h-40 w-full animate-pulse rounded-xl" />

            <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
              {Array.from({ length: 4 }).map((_, i) => (
                <div
                  key={i}
                  className="bg-muted/30 h-[116px] w-full animate-pulse rounded-lg"
                />
              ))}
            </div>

            <div className="bg-muted/30 h-64 w-full animate-pulse rounded-lg" />

            <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
              <div className="bg-muted/30 h-40 w-full animate-pulse rounded-lg" />
              <div className="bg-muted/30 h-40 w-full animate-pulse rounded-lg" />
            </div>

            <div className="bg-muted/30 h-48 w-full animate-pulse rounded-lg" />
          </Stack>
        </div>
      </Page.Body>
    </Page>
  );
}

export function MCPDetailPage(): React.JSX.Element {
  return (
    <RequireScope scope={["mcp:read", "mcp:write"]} level="page">
      <MCPDetailPageInner />
    </RequireScope>
  );
}

function MCPDetailPageInner() {
  const { toolsetSlug } = useParams();

  const {
    data: toolset,
    error: toolsetError,
    isLoading,
  } = useToolset(toolsetSlug, { throwOnError: false });

  if (!toolsetSlug || isNotFoundError(toolsetError)) {
    return <MCPRouteNotFound />;
  }

  if (toolsetError) {
    throw toolsetError;
  }

  if (isLoading || !toolset) {
    return <MCPLoading />;
  }

  return <MCPDetailPageContent toolset={toolset} toolsetSlug={toolsetSlug} />;
}

function MCPRouteNotFound() {
  const routes = useRoutes();

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RouteNotFoundState
          title="MCP server not found"
          description="This MCP server may have been deleted, renamed, or moved out of this project."
          action={
            <routes.mcp.Link>
              <SecondaryRouteAction>Back to MCP servers</SecondaryRouteAction>
            </routes.mcp.Link>
          }
        />
      </Page.Body>
    </Page>
  );
}

type LoadedMcpToolset = NonNullable<ReturnType<typeof useToolset>["data"]>;

function renderMcpDetailTabContent(
  tab: TabValue,
  toolset: LoadedMcpToolset,
): React.ReactNode {
  switch (tab) {
    case "overview":
      return (
        <MCPOverviewTab
          server={{
            kind: "toolset",
            id: toolset.id,
            slug: toolset.slug,
            name: toolset.name,
          }}
        />
      );
    case "tools":
      return <MCPToolsTab toolset={toolset} />;
    case "resources":
      return <MCPResourcesTab toolset={toolset} />;
    case "prompts":
      return <MCPPromptsTab toolset={toolset} />;
    case "authentication":
      return (
        <RequireScope scope="mcp:write" level="page">
          <MCPAuthenticationTab toolset={toolset} />
        </RequireScope>
      );
    case "performance":
      return (
        <RequireScope scope="mcp:write" level="page">
          <MCPPerformanceTab toolset={toolset} />
        </RequireScope>
      );
    case "team-access":
      return (
        <RequireScope scope="mcp:read" level="page">
          <MCPTeamAccessTab resourceId={toolset.id} tools={toolset.tools} />
        </RequireScope>
      );
    case "settings":
      return (
        <RequireScope scope="mcp:write" level="page">
          <MCPSettingsTab toolset={toolset} />
        </RequireScope>
      );
  }
}

function MCPDetailPageContent({
  toolset,
  toolsetSlug,
}: {
  toolset: LoadedMcpToolset;
  toolsetSlug: string;
}) {
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const location = useLocation();
  const isRbacEnabled = telemetry.isFeatureEnabled("gram-rbac") ?? false;

  const activeTab = activeTabFromPath(location.pathname, toolsetSlug);

  if (!activeTab) {
    const initialTab = initialTabFromHash(window.location.hash, isRbacEnabled);
    return (
      <Navigate
        to={mcpDetailTabHref(routes, toolsetSlug, initialTab)}
        replace
      />
    );
  }
  if (activeTab === "team-access" && !isRbacEnabled) {
    return (
      <Navigate
        to={mcpDetailTabHref(routes, toolsetSlug, "overview")}
        replace
      />
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [toolsetSlug]: toolset.name }}
          skipSegments={MCP_DETAIL_TAB_URLS}
        />
      </Page.Header>
      <Page.Body fullWidth className="gap-0">
        {/* Name, status, URL, and Playground live in the sidebar header now */}
        <div className="mx-auto w-full max-w-[1270px] flex-1">
          {renderMcpDetailTabContent(activeTab, toolset)}
        </div>
      </Page.Body>
    </Page>
  );
}

const MCP_SERVER_NAME_MAX_LENGTH = 40;

export function RenameMCPServerButton({
  toolset,
}: {
  toolset: Toolset;
}): React.JSX.Element {
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const updateToolsetMutation = useUpdateToolsetMutation();
  const [isOpen, setIsOpen] = useState(false);
  const [name, setName] = useState(toolset.name);

  useEffect(() => {
    if (isOpen) {
      setName(toolset.name);
    }
  }, [isOpen, toolset.name]);

  const trimmedName = name.trim();
  const nameError =
    trimmedName.length === 0
      ? "Name is required"
      : name.length > MCP_SERVER_NAME_MAX_LENGTH
        ? `Must be ${MCP_SERVER_NAME_MAX_LENGTH} characters or less`
        : null;
  const hasChanges = trimmedName !== toolset.name;
  const canSave = !nameError && hasChanges && !updateToolsetMutation.isPending;

  const handleOpenChange = (open: boolean) => {
    if (!updateToolsetMutation.isPending) {
      setIsOpen(open);
    }
  };

  const handleSave = () => {
    if (!canSave) return;

    updateToolsetMutation.mutate(
      {
        request: {
          slug: toolset.slug,
          updateToolsetRequestBody: {
            name: trimmedName,
          },
        },
      },
      {
        onSuccess: () => {
          void invalidateAllToolset(queryClient);
          void invalidateAllListToolsets(queryClient);
          telemetry.capture("mcp_event", {
            action: "mcp_server_renamed",
            slug: toolset.slug,
          });
          toast.success("MCP server renamed");
          setIsOpen(false);
        },
        onError: (error) => {
          toast.error(
            error instanceof Error ? error.message : "Failed to rename server",
          );
        },
      },
    );
  };

  return (
    <>
      <RequireScope
        scope="mcp:write"
        level="component"
        reason="You don't have permission to rename this MCP server."
        className="shrink-0"
      >
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="tertiary"
              size="sm"
              className="text-muted-foreground hover:text-foreground h-5 w-5 shrink-0 p-0"
              onClick={() => setIsOpen(true)}
              aria-label="Rename MCP server"
            >
              <Button.LeftIcon>
                <Pencil className="h-3 w-3" />
              </Button.LeftIcon>
              <Button.Text className="sr-only">Rename MCP server</Button.Text>
            </Button>
          </TooltipTrigger>
          <TooltipContent>Rename MCP server</TooltipContent>
        </Tooltip>
      </RequireScope>

      <Dialog open={isOpen} onOpenChange={handleOpenChange}>
        <Dialog.Content className="max-w-md">
          <Dialog.Header>
            <Dialog.Title>Rename MCP Server</Dialog.Title>
            <Dialog.Description>
              Update the display name for this MCP server. The URL slug will
              stay the same.
            </Dialog.Description>
          </Dialog.Header>

          <div className="space-y-2 py-1">
            <Label htmlFor="mcp-server-name">Name</Label>
            <Input
              id="mcp-server-name"
              placeholder="My MCP Server"
              value={name}
              onChange={setName}
              onEnter={handleSave}
              maxLength={MCP_SERVER_NAME_MAX_LENGTH}
              disabled={updateToolsetMutation.isPending}
              autoFocus
            />
            <div className="text-muted-foreground flex min-h-5 justify-between gap-4 text-sm">
              <p className="text-destructive">{nameError}</p>
              <p>
                {name.length}/{MCP_SERVER_NAME_MAX_LENGTH}
              </p>
            </div>
          </div>

          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={() => setIsOpen(false)}
              disabled={updateToolsetMutation.isPending}
            >
              Cancel
            </Button>
            <Button onClick={handleSave} disabled={!canSave}>
              {updateToolsetMutation.isPending ? "Saving..." : "Save"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}

type ServerStatus = "disabled" | "private" | "public";

const STATUS_OPTIONS: {
  value: ServerStatus;
  label: string;
  description: string;
  dotClass: string;
  hoverDotClass: string;
}[] = [
  {
    value: "disabled",
    label: "Disabled",
    description: "This server is offline. No users can connect to it",
    dotClass: "bg-amber-400",
    hoverDotClass: "group-hover:bg-amber-400",
  },
  {
    value: "private",
    label: "Private",
    description:
      "Only users with a platform API Key from this project can read the tools hosted by this server.",
    dotClass: "bg-blue-400",
    hoverDotClass: "group-hover:bg-blue-400",
  },
  {
    value: "public",
    label: "Public",
    description:
      "Anyone with the URL can read the tools hosted by this server. Authentication is still required to use the tools.",
    dotClass: "bg-green-400",
    hoverDotClass: "group-hover:bg-green-400",
  },
];

export function MCPStatusDropdown({
  toolset,
}: {
  toolset: Toolset;
}): React.JSX.Element {
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write");
  const queryClient = useQueryClient();
  const [dropdownOpen, setDropdownOpen] = useState(false);
  const [pendingStatus, setPendingStatus] = useState<ServerStatus | null>(null);
  const [publicWarningPending, setPublicWarningPending] = useState<{
    target: ServerStatus;
    sourceStatus: ServerStatus;
  } | null>(null);
  const updateToolsetMutation = useUpdateToolsetMutation();
  const telemetry = useTelemetry();

  // Fetch data needed to detect system-provided vars on the attached env.
  const { data: environmentsData } = useListEnvironments();
  const environments = environmentsData?.environments ?? [];
  const { data: mcpMetadataData, isLoading: mcpMetadataLoading } =
    useGetMcpMetadata({ toolsetSlug: toolset.slug }, undefined, {
      retry: false,
      throwOnError: false, // Expected 404 when no metadata exists
    });
  const mcpMetadata = mcpMetadataData?.metadata;

  const attachedEnvironment = mcpMetadata?.defaultEnvironmentId
    ? (environments.find((e) => e.id === mcpMetadata.defaultEnvironmentId) ??
      null)
    : null;

  const envVars = useEnvironmentVariables(toolset, environments, mcpMetadata);
  const systemVarNames = useMemo(
    () =>
      attachedEnvironment
        ? getSystemProvidedVariables(envVars, attachedEnvironment.slug)
        : [],
    [envVars, attachedEnvironment],
  );

  // While either the metadata or the environments query is still loading we
  // can't resolve the attached env → can't know whether system vars exist →
  // disable the "Public" option to prevent a silent bypass of the warning
  // dialog. Covers the race where metadata resolves first and `environments`
  // is still `[]`, which would otherwise make `attachedEnvironment` null and
  // `systemVarNames` empty even when there are system vars on an attached env.
  // We intentionally do NOT fail-closed on query errors: the metadata endpoint
  // returns 404 when no metadata row exists for a toolset (a common, safe
  // state meaning "no attached env"), and `retry: false` would otherwise lock
  // the option permanently on that 404. Other call sites (MCPAuthenticationTab,
  // MCPEnvironmentSettings) treat missing metadata the same way.
  const publicOptionUnavailable = mcpMetadataLoading || !environmentsData;

  const currentStatus: ServerStatus = !toolset.mcpEnabled
    ? "disabled"
    : toolset.mcpIsPublic
      ? "public"
      : "private";

  const applyStatus = (status: ServerStatus) => {
    const updates =
      status === "disabled"
        ? { mcpEnabled: false }
        : { mcpEnabled: true, mcpIsPublic: status === "public" };

    updateToolsetMutation.mutate(
      {
        request: {
          slug: toolset.slug,
          updateToolsetRequestBody: updates,
        },
      },
      {
        onSuccess: () => {
          void invalidateAllToolset(queryClient);
          void invalidateAllGetPeriodUsage(queryClient);
          telemetry.capture("mcp_event", {
            action:
              status === "disabled"
                ? "mcp_disabled"
                : status === "public"
                  ? "mcp_made_public"
                  : "mcp_made_private",
            slug: toolset.slug,
            system_vars_warned:
              status === "public" ? systemVarNames.length > 0 : undefined,
          });
          const label =
            status === "disabled"
              ? "MCP server disabled"
              : status === "public"
                ? "MCP server set to public"
                : "MCP server set to private";
          toast.success(label);
        },
        onError: (error) => {
          toast.error(
            error instanceof Error
              ? error.message
              : "Failed to update server status",
          );
        },
      },
    );
  };

  const handleSelect = (status: ServerStatus) => {
    if (status === currentStatus) return;
    setDropdownOpen(false);

    const goingPublic = status === "public";
    const needsEnableDialog =
      status === "disabled" || currentStatus === "disabled";
    const needsPublicWarning = goingPublic && systemVarNames.length > 0;

    // Defer state changes until after the dropdown has fully closed to avoid
    // Radix focus-trap conflicts (same pattern as before).
    setTimeout(() => {
      if (needsPublicWarning) {
        // Show the system-vars warning first. If the user confirms, we chain to
        // ServerEnableDialog when the transition also requires enablement.
        setPublicWarningPending({
          target: status,
          sourceStatus: currentStatus,
        });
      } else if (needsEnableDialog) {
        setPendingStatus(status);
      } else {
        applyStatus(status);
      }
    }, 0);
  };

  const handlePublicWarningConfirm = () => {
    const pending = publicWarningPending;
    setPublicWarningPending(null);
    if (!pending) return;
    // Use the source status captured when the dialog opened, not the live
    // currentStatus — the toolset query may have revalidated in the meantime.
    if (pending.sourceStatus === "disabled") {
      setPendingStatus(pending.target);
    } else {
      applyStatus(pending.target);
    }
  };

  const currentLabel =
    currentStatus === "disabled"
      ? "Disabled"
      : currentStatus === "public"
        ? "Public"
        : "Private";

  return (
    <>
      <DropdownMenu open={dropdownOpen} onOpenChange={setDropdownOpen}>
        <DropdownMenuTrigger asChild disabled={!canWrite}>
          <button
            type="button"
            disabled={!canWrite}
            className="text-foreground hover:bg-muted trans border-border flex w-fit items-center gap-2 rounded-md border px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
          >
            <span
              className={cn(
                "h-2 w-2 shrink-0 rounded-full",
                STATUS_OPTIONS.find((option) => option.value === currentStatus)
                  ?.dotClass,
              )}
            />
            {currentLabel}
            <ChevronDown className="text-muted-foreground h-3 w-3" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-[320px] p-1">
          {STATUS_OPTIONS.map((option) => (
            <DropdownMenuItem
              key={option.value}
              onSelect={() => handleSelect(option.value)}
              disabled={option.value === "public" && publicOptionUnavailable}
              className="group flex cursor-pointer items-start gap-2.5 rounded-md p-2"
            >
              {option.value === currentStatus ? (
                <span
                  className={cn(
                    "mt-1 flex size-3.5 shrink-0 items-center justify-center rounded-full",
                    option.dotClass,
                  )}
                >
                  <Check
                    className="text-background h-2.5 w-2.5"
                    strokeWidth={4}
                  />
                </span>
              ) : (
                <span
                  className={cn(
                    "mt-1 size-3.5 shrink-0 rounded-full transition-colors",
                    "bg-muted",
                    option.hoverDotClass,
                  )}
                />
              )}
              <div className="flex-1">
                <span className="block font-mono text-xs font-semibold tracking-wide uppercase">
                  {option.label}
                </span>
                <span className="text-muted-foreground text-xs">
                  {option.value === "public" && publicOptionUnavailable
                    ? "Loading environment data…"
                    : option.description}
                </span>
              </div>
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
      <PublicMcpWarningDialog
        isOpen={publicWarningPending !== null}
        onClose={() => setPublicWarningPending(null)}
        onConfirm={handlePublicWarningConfirm}
        isLoading={updateToolsetMutation.isPending}
        environmentSlug={attachedEnvironment?.slug ?? ""}
        variableNames={systemVarNames}
      />
      <ServerEnableDialog
        isOpen={pendingStatus !== null}
        onClose={() => setPendingStatus(null)}
        onConfirm={() => {
          if (pendingStatus) applyStatus(pendingStatus);
        }}
        isLoading={updateToolsetMutation.isPending}
        currentlyEnabled={currentStatus !== "disabled"}
        targetIsPublic={pendingStatus === "public"}
      />
    </>
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
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write");
  const charCount = form.instructionsHandlers.value?.length ?? 0;
  const overLimit = charCount > INSTRUCTIONS_SOFT_LIMIT;

  return (
    <Stack gap={3}>
      <div className="relative">
        <Textarea
          placeholder={`Describe how your tools work together, required workflows,\nand any constraints (rate limits, auth requirements, etc.).\n\nKeep it concise — don't repeat individual tool descriptions.`}
          className="min-h-[150px] w-full"
          value={form.instructionsHandlers.value ?? ""}
          onChange={form.instructionsHandlers.onChange}
          disabled={!canWrite}
        />
        {charCount > 0 && (
          <span
            className={cn(
              "absolute right-3 bottom-2 text-xs",
              overLimit ? "text-destructive" : "text-muted-foreground",
            )}
          >
            {charCount.toLocaleString()} /{" "}
            {INSTRUCTIONS_SOFT_LIMIT.toLocaleString()}
          </span>
        )}
      </div>
      {canWrite && (
        <Stack direction="horizontal" gap={2} justify="end">
          <GenerateInstructionsButton toolset={toolset} form={form} />
          <Button
            onClick={() => {
              void (async () => {
                try {
                  await form.saveAsync();
                  toast.success("Server instructions saved.");
                } catch {
                  toast.error("Failed to save instructions.");
                }
              })();
            }}
            disabled={isLoading || !form.instructionsDirty}
            size="sm"
          >
            <Button.Text>Save</Button.Text>
          </Button>
        </Stack>
      )}
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
  const model = useModel(DEFAULT_MODEL);

  const tools = fullToolset?.tools ?? [];

  const handleGenerate = async () => {
    if (tools.length === 0) {
      return;
    }

    setGenerating(true);
    try {
      const res = await generateText({
        model,
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
      onClick={() => void handleGenerate()}
      disabled={generating || tools.length === 0}
    >
      <Button.LeftIcon>
        <Icon name="wand-sparkles" className="h-4 w-4" />
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
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write");
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const client = useSdkClient();
  const routes = useRoutes();
  const { data: fullToolset, refetch } = useToolset(toolset.slug);

  // Read-only tool filtering ("scopes") view. The resolved variation group, when
  // present, mirrors what the runtime ?tags= filter exposes.
  const { data: toolFilters } = useListToolsetToolFilters(
    { slug: toolset.slug },
    undefined,
    { throwOnError: false },
  );
  const filteringEnabled = toolFilters?.filteringEnabled ?? false;
  const [activeTag, setActiveTag] = useState<string | null>(null);

  const [addToolsDialogOpen, setAddToolsDialogOpen] = useState(false);

  const tools = fullToolset?.tools ?? [];

  // Validate the selected tag against the current filters (derived during render
  // so a refetch that drops the selected scope cleanly falls back to "all tools"
  // without a stale chip or a reset effect).
  const effectiveActiveTag = useMemo(() => {
    if (!toolFilters || activeTag === null) return null;
    if (activeTag === EXCLUDED_TAG_KEY) {
      return toolFilters.excluded.length > 0 ? EXCLUDED_TAG_KEY : null;
    }
    return toolFilters.scopes.some((s) => s.tag === activeTag)
      ? activeTag
      : null;
  }, [activeTag, toolFilters]);

  // When a scope chip is active, restrict the list below to that scope's tools
  // (or the excluded set), matched by URN so variation renames don't break it.
  const activeFilterUrns = useMemo(() => {
    if (!effectiveActiveTag || !toolFilters) return null;
    if (effectiveActiveTag === EXCLUDED_TAG_KEY) {
      return new Set(toolFilters.excluded.map((tool) => tool.toolUrn));
    }
    const scope = toolFilters.scopes.find((s) => s.tag === effectiveActiveTag);
    return scope ? new Set(scope.tools.map((tool) => tool.toolUrn)) : null;
  }, [effectiveActiveTag, toolFilters]);

  // Check if this is an external MCP proxy server
  const isExternalMcpProxy = fullToolset?.kind === "external-mcp-proxy";

  // Check if we have orphaned tool URNs (URNs exist but tools were deleted)
  const hasOrphanedTools =
    (fullToolset?.toolUrns?.length ?? 0) > 0 &&
    fullToolset?.rawTools.length === 0;

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      telemetry.capture("toolset_event", { action: "toolset_updated" });
      void refetch();
      void invalidateAllToolset(queryClient);
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

  const groupKeysJoined = grouped.map((group) => group.key).join(",");
  // Set initial selected groups when the tool list resolves
  useEffect(() => {
    setSelectedGroups(grouped.map((group) => group.key));
    // eslint-disable-next-line react-hooks/exhaustive-deps -- recalculate only when the set of group keys changes
  }, [groupKeysJoined]);

  const { updateTool, isUpdating } = useToolUpdate({
    telemetryEvent: "toolset_event",
    // Refresh the toolset and the tool filtering scopes. Editing a tool's tags
    // can add or remove filter scopes, so the read-only filtering panel above
    // must be invalidated too — otherwise new tags only appear after a reload.
    onSuccess: () => {
      void refetch();
      void invalidateListToolsetToolFilters(queryClient, [
        { slug: toolset.slug },
      ]);
    },
  });

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
  if (activeFilterUrns) {
    toolsToDisplay = toolsToDisplay.filter((tool) =>
      activeFilterUrns.has(tool.toolUrn),
    );
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
            {canWrite && (
              <routes.customTools.Link>
                <Button variant="secondary" size="sm">
                  <Button.Text>Custom Tools</Button.Text>
                </Button>
              </routes.customTools.Link>
            )}
            {canWrite && (
              <Button onClick={() => setAddToolsDialogOpen(true)} size="sm">
                <Button.LeftIcon>
                  <Icon name="plus" className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Add Tools</Button.Text>
              </Button>
            )}
          </Stack>
        </Stack>
      )}

      {/* Read-only tool filtering scopes panel (only when filtering enabled) */}
      {!isExternalMcpProxy && filteringEnabled && toolFilters && (
        <MCPToolFilterScopesPanel
          filters={toolFilters}
          activeTag={effectiveActiveTag}
          onSelectTag={setActiveTag}
        />
      )}

      {/* Group filter */}
      {!isExternalMcpProxy && groupFilterItems.length > 1 && (
        <div className="relative mb-4 w-full">
          <MultiSelect
            options={groupFilterItems}
            defaultValue={groupFilterItems.map((item) => item.value)}
            onValueChange={setSelectedGroups}
            placeholder="Filter tools"
            className="capitalize"
            hideSelectAll={true}
            autoSize={true}
          />
        </div>
      )}

      {/* Tools list or empty state */}
      {hasOrphanedTools ? (
        <Stack gap={4} align="center" className="py-12">
          <div className="max-w-md text-center">
            <AlertTriangle className="text-warning mx-auto mb-4 h-12 w-12" />
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
          onToolUpdate={canWrite ? updateTool : undefined}
          isToolUpdating={isUpdating}
          onToolsRemove={canWrite ? handleToolsRemove : undefined}
          onTestInPlayground={handleTestInPlayground}
          readOnly={!canWrite}
        />
      ) : (
        <ToolsetEmptyState
          toolsetSlug={toolset.slug}
          onAddTools={canWrite ? () => setAddToolsDialogOpen(true) : undefined}
        />
      )}

      {/* Add Tools Dialog */}
      {fullToolset && !isExternalMcpProxy && (
        <AddToolsDialog
          open={addToolsDialogOpen}
          onOpenChange={setAddToolsDialogOpen}
          toolset={fullToolset}
          onAddTools={(toolUrns) => {
            void (async (toolUrns) => {
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
              void invalidateAllToolset(queryClient);
            })(toolUrns);
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
      void invalidateAllToolset(queryClient);
    },
  });

  if (!fullToolset) return null;

  return (
    <Page.Section>
      <Page.Section.Title>Resources</Page.Section.Title>
      <Page.Section.Body>
        <ResourcesTabContent
          toolset={fullToolset}
          updateToolsetMutation={updateToolsetMutation}
        />
      </Page.Section.Body>
    </Page.Section>
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
      void invalidateAllToolset(queryClient);
    },
  });

  if (!fullToolset) return null;

  return (
    <Page.Section>
      <Page.Section.Title>Prompts</Page.Section.Title>
      <Page.Section.Body>
        <PromptsTabContent
          toolset={fullToolset}
          updateToolsetMutation={updateToolsetMutation}
        />
      </Page.Section.Body>
    </Page.Section>
  );
}

/**
 * Settings Tab - Visibility, Slug, Custom Domain, Actions, Danger Zone
 */
function MCPSettingsTab({ toolset }: { toolset: Toolset }) {
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write");
  const telemetry = useTelemetry();
  const queryClient = useQueryClient();
  const productTier = useProductTier();
  const { orgSlug } = useParams();
  const { domain } = useCustomDomain();
  const routes = useRoutes();
  const client = useSdkClient();
  const toolsets = useToolsets();
  const { data: deploymentResult, refetch: refetchDeployment } =
    useLatestDeployment();
  const deployment = deploymentResult?.deployment;

  const metadataResult = useGetMcpMetadata(
    { toolsetSlug: toolset.slug },
    undefined,
    {
      retry: (_, err) => {
        if (err instanceof GramError && err.statusCode === 404) {
          return false;
        }
        return true;
      },
      throwOnError: false,
    },
  );
  const instructionsForm = useMcpMetadataMetadataForm(
    { kind: "toolset", toolsetSlug: toolset.slug },
    metadataResult.data?.metadata,
  );
  const instructionsLoading =
    metadataResult.isLoading || instructionsForm.isLoading;

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

      void invalidateAllToolset(queryClient, { refetchType: "none" });
      void invalidateAllGetPeriodUsage(queryClient);
      void refetchDeployment();
      // Wait for the toolset list to refresh before navigating so the
      // listing page never renders a card for the deleted toolset (which
      // would trigger a per-card getBySlug refetch that 404s).
      await toolsets.refetch();

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
      void invalidateAllToolset(queryClient);
      toast.success("MCP settings saved successfully");
      telemetry.capture("mcp_event", {
        action: "mcp_settings_saved",
        slug: toolset.slug,
      });
    },
    onError: () => {
      // Discard staged changes
      setMcpSlug(toolset.mcpSlug || "");
    },
  });

  // TODO(AGE-1902): replace the single-slug + single-customDomainId fields
  // below with the shared Endpoints split-surface (Gram endpoint + N
  // custom-domain endpoint rows) introduced for mcp_servers-backed servers
  // under `client/dashboard/src/pages/mcp/x/MCPServerDetails.tsx`. Once the
  // Hosted MCP cards source from mcp_servers/mcp_endpoints, slug + custom
  // domain handling collapses into that shared surface and the
  // `toolset.mcpSlug` / `toolset.customDomainId` fields can be deprecated.
  const [mcpSlug, setMcpSlug] = useState(toolset.mcpSlug || "");
  const [isCustomDomainModalOpen, setIsCustomDomainModalOpen] = useState(false);

  const mcpSlugError = useMcpSlugValidation(mcpSlug, toolset.mcpSlug);

  const { url: _mcpUrl, customServerURL } = useMcpUrl(toolset);

  const handleMcpSlugChange = (value: string) => {
    value = value.slice(0, 40);
    setMcpSlug(value);
  };

  const linkDomainButton = canWrite &&
    domain &&
    domain.activated &&
    domain.verified && (
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
      <RequireScope scope="project:write" level="component">
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
      </RequireScope>
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
      }}
    >
      Discard
    </Button>
  );

  return (
    <Stack gap={0} className="mb-4">
      <PageSection
        heading="Server Instructions"
        description="Instructions returned to LLMs when they connect to your MCP server. Describe how your tools work together, required workflows, and any constraints."
      >
        <ServerInstructionsSection
          toolset={toolset}
          form={instructionsForm}
          isLoading={instructionsLoading}
        />
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
                  className="w-full rounded border px-2 py-1"
                  placeholder="Enter MCP Slug"
                  value={mcpSlug}
                  onChange={handleMcpSlugChange}
                  maxLength={40}
                  requiredPrefix={`${orgSlug}-`}
                  disabled={!canWrite}
                />
              ) : (
                <Input
                  className="w-full rounded border px-2 py-1"
                  placeholder="Enter MCP Slug"
                  value={mcpSlug}
                  onChange={handleMcpSlugChange}
                  maxLength={40}
                  disabled={!toolset.customDomainId || !canWrite}
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

      <MCPPublishingSection toolset={toolset} />

      <MCPToolFilteringSection
        className="mb-8"
        target={{
          kind: "toolset",
          slug: toolset.slug,
          currentGroupId: toolset.toolVariationsGroupId,
        }}
      />

      <PageSection
        heading="Actions"
        description="Export your MCP server configuration."
      >
        <div>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="secondary"
                size="md"
                onClick={() => void handleExportJson()}
                disabled={!toolset?.mcpEnabled || !toolset?.mcpSlug}
              >
                <Button.LeftIcon>
                  <Download className="h-4 w-4" />
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
        </div>
      </PageSection>

      {/* Danger Zone */}
      <div className="border-destructive/30 mt-8 rounded-lg border p-6">
        <Type variant="subheading" className="text-destructive mb-1">
          Danger Zone
        </Type>
        <Type muted small className="mb-4">
          Permanently delete this MCP server. This action cannot be undone.
        </Type>
        <RequireScope scope="mcp:write" level="component">
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
        </RequireScope>
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
              <code className="bg-muted rounded px-1 py-0.5 font-mono font-bold">
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
                onClick={() => void handleDeleteMcpServer()}
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
    </Stack>
  );
}

// MCPPublishingSection wraps the shared publishing section for toolset-backed
// MCP servers. The mcp_server-backed variant lives on the Remote MCP server
// settings page; both share MCPPublishingSection.
function MCPPublishingSection({ toolset }: { toolset: Toolset }) {
  return (
    <SharedMCPPublishingSection
      target={{
        kind: "toolset",
        toolsetId: toolset.id,
        mcpSlug: toolset.mcpSlug ?? undefined,
      }}
      canPublish={Boolean(toolset.mcpEnabled && toolset.mcpSlug)}
      disabledMessage="Enable this MCP server before publishing it to a collection."
    />
  );
}

export function PageSection({
  heading,
  description,
  featureType,
  action,
  headingExtra,
  children,
  className,
}: {
  heading: string;
  description: string;
  fullWidth?: boolean;
  featureType?: "experimental" | "beta";
  action?: React.ReactNode;
  headingExtra?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}): React.JSX.Element {
  return (
    <Stack gap={2} className={cn("mb-8", className)}>
      <div className="flex items-center justify-between">
        <Heading variant="h3" className="flex items-center">
          {heading}
          {featureType && (
            <Badge variant="warning" className="ml-2">
              {featureType}
            </Badge>
          )}
          {headingExtra}
        </Heading>
        {action}
      </div>
      <Type muted small className="max-w-2xl">
        {description}
      </Type>
      {children}
    </Stack>
  );
}

export function OAuthDetailsModal({
  isOpen,
  onClose,
  toolset,
  onEditRequest,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolset: Toolset;
  onEditRequest: () => void;
}): React.JSX.Element {
  const { url: mcpUrl } = useMcpUrl(toolset);
  const queryClient = useQueryClient();

  const removeOAuthMutation = useRemoveOAuthServerMutation({
    onSuccess: () => {
      void invalidateAllToolset(queryClient);
      onClose();
    },
  });

  const isGramOAuth =
    toolset.oauthProxyServer?.oauthProxyProviders?.[0]?.providerType === "gram";

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="flex max-h-[80vh] max-w-2xl flex-col">
        <Dialog.Header className="shrink-0">
          <Dialog.Title>
            {toolset.externalOauthServer
              ? "External OAuth Configuration"
              : isGramOAuth
                ? "Platform OAuth Configuration"
                : "OAuth Proxy Configuration"}
          </Dialog.Title>
        </Dialog.Header>
        <div className="flex-1 overflow-y-auto">
          <Stack gap={4}>
            {toolset.oauthProxyServer && isGramOAuth && (
              <>
                <div>
                  <Type className="font-medium">Platform OAuth is Active</Type>
                </div>
                <Stack gap={2} className="">
                  <Type className="mb-2">
                    Platform users with access to your organization can use this
                    MCP server.
                  </Type>
                  {toolset.oauthProxyServer.oauthProxyProviders?.[0]
                    ?.environmentSlug && (
                    <div>
                      <Type small className="text-muted-foreground font-medium">
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
                  <div className="flex items-center gap-2">
                    <Button
                      variant="tertiary"
                      size="sm"
                      onClick={() => {
                        onClose();
                        onEditRequest();
                      }}
                    >
                      <Pencil className="mr-2 h-4 w-4" />
                      Edit
                    </Button>
                    <Button
                      variant="tertiary"
                      size="sm"
                      className="hover:bg-destructive border-none hover:text-white"
                      onClick={() =>
                        removeOAuthMutation.mutate({
                          request: {
                            slug: toolset.slug,
                          },
                        })
                      }
                    >
                      <Trash2 className="mr-2 h-4 w-4" />
                      Unlink
                    </Button>
                  </div>
                </div>
                <Stack gap={2} className="pl-4">
                  <div>
                    <Type small className="text-muted-foreground font-medium">
                      Server Slug:
                    </Type>
                    <CodeBlock className="mt-1">
                      {toolset.oauthProxyServer.slug}
                    </CodeBlock>
                  </div>
                  {toolset.oauthProxyServer.audience && (
                    <div>
                      <Type small className="text-muted-foreground font-medium">
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
                          className="text-muted-foreground font-medium"
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
                          className="text-muted-foreground font-medium"
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
                              className="text-muted-foreground font-medium"
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
                              className="text-muted-foreground font-medium"
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
                            className="text-muted-foreground font-medium"
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
                      <Trash2 className="h-4 w-4" />
                    </Button.LeftIcon>
                    <Button.Text className="sr-only">Remove OAuth</Button.Text>
                  </Button>
                </div>
                <Stack gap={2} className="pl-4">
                  <div>
                    <Type small className="text-muted-foreground font-medium">
                      External OAuth Server Slug:
                    </Type>
                    <CodeBlock className="mt-1">
                      {toolset.externalOauthServer.slug}
                    </CodeBlock>
                  </div>
                  <div>
                    <Type small className="text-muted-foreground font-medium">
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
                    <Type small className="text-muted-foreground font-medium">
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
              <Trash2 className="mr-2 h-4 w-4" />
              Unlink
            </Button>
          </Dialog.Footer>
        )}
      </Dialog.Content>
    </Dialog>
  );
}

export function GramOAuthProxyModal({
  isOpen,
  onClose,
  toolset,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolset: Toolset;
}): React.JSX.Element {
  const telemetry = useTelemetry();
  const queryClient = useQueryClient();

  const addOAuthProxyMutation = useAddOAuthProxyServerMutation({
    onSuccess: () => {
      void invalidateAllToolset(queryClient);
      toast.success("Platform OAuth configured successfully");
      telemetry.capture("mcp_event", {
        action: "gram_oauth_proxy_configured",
        slug: toolset.slug,
      });
      onClose();
    },
    onError: (error) => {
      console.error("Failed to configure Platform OAuth:", error);
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to configure Platform OAuth",
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
      <Dialog.Content className="max-h-[90vh] max-w-2xl overflow-hidden">
        <Dialog.Header>
          <Dialog.Title>Platform OAuth</Dialog.Title>
        </Dialog.Header>

        <div className="max-h-[60vh] space-y-4 overflow-auto">
          <div>
            <Type className="mb-2 font-medium">
              Platform OAuth Configuration
            </Type>
            <Type small className="mb-4">
              Configure Platform OAuth to let users with access to your
              organization use this MCP server. Users will authenticate using
              their platform credentials.
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
              : "Enable Platform OAuth"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

export { ConnectOAuthModal, EditOAuthProxyModal } from "./oauth-wizard";
