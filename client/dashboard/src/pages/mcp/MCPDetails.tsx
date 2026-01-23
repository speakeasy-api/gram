import { Block, BlockInner } from "@/components/block";
import { CodeBlock } from "@/components/code";
import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { ConfigForm } from "@/components/mcp_install_page/config_form";
import { ServerEnableDialog } from "@/components/server-enable-dialog";
import { BigToggle } from "@/components/ui/big-toggle";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Link } from "@/components/ui/link";
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
import { isHttpTool, Toolset } from "@/lib/toolTypes";
import { cn, getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import {
  invalidateAllGetPeriodUsage,
  invalidateAllToolset,
  invalidateGetMcpMetadata,
  useAddExternalOAuthServerMutation,
  useAddOAuthProxyServerMutation,
useGetDomain,
  useGetMcpMetadata,
  useLatestDeployment,
  useRemoveOAuthServerMutation,
  useUpdateSecurityVariableDisplayNameMutation,
  useUpdateToolsetMutation,
} from "@gram/client/react-query";
import { useCustomDomain, useMcpUrl } from "@/hooks/useToolsetUrl";
import { Badge, Button, Grid, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Check, CheckCircleIcon, Globe, LockIcon, Pencil, Trash2, X, XCircleIcon } from "lucide-react";
import React, { useEffect, useState } from "react";
import { Outlet, useParams } from "react-router";
import { toast } from "sonner";
import { EnvironmentDropdown } from "../environments/EnvironmentDropdown";
import { onboardingStepStorageKeys } from "../home/Home";
import { ToolsetCard } from "../toolsets/ToolsetCard";
import { MCPHeroIllustration } from "@/components/sources/SourceCardIllustrations";
import { Page } from "@/components/page-layout";

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

    console.log("Deleting toolset:", toolset.slug, "toolUrns:", toolset.toolUrns);

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
      toast.error(`Failed to delete: ${error instanceof Error ? error.message : "Unknown error"}`, { id: toastId });
    }
  };

  useEffect(() => {
    localStorage.setItem(onboardingStepStorageKeys.configure, "true");
  }, []);

  // TODO: better loading state
  if (isLoading || !toolset) {
    return <div>Loading...</div>;
  }

  const availableOAuthAuthCode =
    toolset?.oauthEnablementMetadata?.oauth2SecurityCount > 0;

  let statusBadge = null;
  if (!toolset.mcpEnabled) {
    statusBadge = (
      <Badge variant="secondary" className="flex items-center gap-1">
        <XCircleIcon className="w-3 h-3" />
        Disabled
      </Badge>
    );
  } else if (toolset.mcpIsPublic) {
    statusBadge = (
      <Badge variant="secondary" className="flex items-center gap-1">
        <CheckCircleIcon className="w-3 h-3 text-green-600" />
        Public
      </Badge>
    );
  } else {
    statusBadge = (
      <Badge variant="secondary" className="flex items-center gap-1">
        <LockIcon className="w-3 h-3" />
        Private
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
        <MCPHeroIllustration mcpUrl={mcpUrl || ""} toolsetSlug={toolset.slug} />

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
                variant="ghost"
                size="sm"
                onClick={() => {
                  if (mcpUrl) {
                    navigator.clipboard.writeText(mcpUrl);
                    toast.success("URL copied to clipboard");
                  }
                }}
                className="shrink-0"
              >
                <Button.Icon>
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
                </Button.Icon>
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
                      {isOAuthConnected ? "OAuth Connected" : "Configure OAuth"}
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
                  <Trash2 className="w-4 h-4" />
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
    <Stack
      className={cn(
        "mb-4",
        !toolset.mcpEnabled && "blur-[2px] pointer-events-none",
      )}
    >
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
 * Tools Tab - Coming soon placeholder
 */
function MCPToolsTab({ toolset }: { toolset: Toolset }) {
  return (
    <Stack
      className={cn(
        "mb-4",
        !toolset.mcpEnabled && "blur-[2px] pointer-events-none",
      )}
    >
      <div className="flex items-center justify-center h-64 border rounded-lg bg-muted/20">
        <Stack align="center" gap={2}>
          <Type muted>Tools management coming soon</Type>
        </Stack>
      </div>
    </Stack>
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

  const { url: mcpUrl, customServerURL } = useMcpUrl(toolset);

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
    <Stack
      className={cn(
        "mb-4",
        !toolset.mcpEnabled && "blur-[2px] pointer-events-none",
      )}
    >
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
                    <Button.Icon>
                      <Trash2 className="w-4 h-4" />
                    </Button.Icon>
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
