import { CodeBlock } from "@/components/code";
import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { ServerEnableDialog } from "@/components/server-enable-dialog";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { ExternalLink, Trash2 } from "lucide-react";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Link } from "@/components/ui/link";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { TextArea } from "@/components/ui/textarea";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useProject, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { cn, getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Toolset, ToolsetEntry } from "@gram/client/models/components";
import {
  invalidateAllGetPeriodUsage,
  invalidateAllToolset,
  useAddExternalOAuthServerMutation,
  useGetDomain,
  useListTools,
  useRemoveOAuthServerMutation,
  useToolsetSuspense,
  useUpdateToolsetMutation,
} from "@gram/client/react-query";
import { Grid, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Globe } from "lucide-react";
import React, { useEffect, useState } from "react";
import { Outlet, useParams } from "react-router";
import { toast } from "sonner";
import { onboardingStepStorageKeys } from "../home/Home";
import { Block, BlockInner } from "../toolBuilder/components";
import { ToolsetCard } from "../toolsets/ToolsetCard";

export function MCPDetailsRoot() {
  return <Outlet />;
}

export function MCPDetailPage() {
  const { toolsetSlug } = useParams();

  const toolset = useToolsetSuspense({ slug: toolsetSlug! });
  const activeOAuthAuthCode =
    toolset.data.securityVariables?.some(
      (secVar) =>
        secVar.type === "oauth2" &&
        secVar.oauthTypes?.includes("authorization_code")
    ) ?? false;
  const isOAuthConnected = !!(
    toolset.data.oauthProxyServer || toolset.data.externalOauthServer
  );
  const [isOAuthModalOpen, setIsOAuthModalOpen] = useState(false);
  const [isOAuthDetailsModalOpen, setIsOAuthDetailsModalOpen] = useState(false);

  useEffect(() => {
    localStorage.setItem(onboardingStepStorageKeys.configure, "true");
  }, []);

  return (
    <Stack>
      <Stack
        direction="horizontal"
        align="center"
        className="mb-8 justify-between"
      >
        <Heading variant="h2">MCP Details</Heading>
        <Stack direction="horizontal" gap={2}>
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                {!activeOAuthAuthCode || !toolset.data.mcpIsPublic ? (
                  <span className="inline-block">
                    <Button variant="secondary" size="md" disabled={true}>
                      {isOAuthConnected ? "OAuth Connected" : "Connect OAuth"}
                    </Button>
                  </span>
                ) : (
                  <Button variant="secondary"
                    size="md"
                    onClick={() =>
                      isOAuthConnected
                        ? setIsOAuthDetailsModalOpen(true)
                        : setIsOAuthModalOpen(true)
                    }
                  >
                    {isOAuthConnected ? "OAuth Connected" : "Connect OAuth"}
                  </Button>
                )}
              </TooltipTrigger>
              {(!activeOAuthAuthCode || !toolset.data.mcpIsPublic) && (
                <TooltipContent>
                  {!activeOAuthAuthCode
                    ? "This MCP server does not require the OAuth authorization code flow"
                    : "This MCP Server must not be private to enable OAuth"}
                </TooltipContent>
              )}
            </Tooltip>
          </TooltipProvider>
          <MCPEnableButton toolset={toolset.data} />
        </Stack>
      </Stack>
      <PageSection
        heading="Source Toolset"
        description="MCP servers expose the contents of a single toolset. To change the
          tools or prompts exposed by this MCP server, update the source toolset
          below."
        className="max-w-2xl"
      >
        <ToolsetCard toolset={toolset.data} />
      </PageSection>
      <MCPDetails toolset={toolset.data} />
      <ConnectOAuthModal
        isOpen={isOAuthModalOpen}
        onClose={() => setIsOAuthModalOpen(false)}
        toolsetSlug={toolset.data.slug}
        toolset={toolset.data}
      />
      <OAuthDetailsModal
        isOpen={isOAuthDetailsModalOpen}
        onClose={() => setIsOAuthDetailsModalOpen(false)}
        toolset={toolset.data}
      />
    </Stack>
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
            toolset.mcpEnabled ? "MCP server disabled" : "MCP server enabled"
          );
        },
      }
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

export function useCustomDomain() {
  const {
    data: domain,
    isLoading,
    refetch,
  } = useGetDomain(undefined, undefined, {
    refetchOnWindowFocus: false,
    retry: false,
    throwOnError: false,
  });

  return { domain: domain, refetch: refetch, isLoading };
}

export function useMcpUrl(
  toolset:
    | Pick<
        ToolsetEntry,
        "slug" | "customDomainId" | "mcpSlug" | "defaultEnvironmentSlug"
      >
    | undefined
) {
  const { domain } = useCustomDomain();
  const project = useProject();

  if (!toolset) return { url: undefined, customServerURL: undefined };

  // Determine which server URL to use
  let customServerURL: string | undefined;
  if (domain && toolset.customDomainId && domain.id == toolset.customDomainId) {
    customServerURL = `https://${domain.domain}`;
  }

  const urlSuffix = toolset.mcpSlug
    ? toolset.mcpSlug
    : `${project.slug}/${toolset.slug}/${toolset.defaultEnvironmentSlug}`;
  const mcpUrl = `${
    toolset.mcpSlug && customServerURL ? customServerURL : getServerURL()
  }/mcp/${urlSuffix}`;

  return {
    url: mcpUrl,
    customServerURL,
    pageUrl: `${mcpUrl}/install`,
  };
}

export function MCPDetails({ toolset }: { toolset: Toolset }) {
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
          "maximum number of public MCP servers for your account type"
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

  const linkDomainButton = domain && (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="secondary"
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
    </TooltipProvider>
  );

  const customDomain =
    domain && session.gramAccountType !== "free" && !toolset.customDomainId ? (
      linkDomainButton
    ) : (
      <Button variant="secondary"
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

  const saveButton = (
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
    <Button variant="tertiary"
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
    const classes = {
      both: "px-2 py-1 rounded-sm border-1 w-full",
      active: "border-border bg-card text-foreground",
      inactive:
        "border-transparent text-muted-foreground hover:bg-card hover:cursor-pointer hover:border-border hover:text-foreground",
      activeText: "text-foreground!",
      inactiveText: "text-muted-foreground! italic",
    };

    const onToggle = () => {
      setMcpIsPublic(!isPublic);
      updateToolsetMutation.mutate({
        request: {
          slug: toolset.slug,
          updateToolsetRequestBody: { mcpIsPublic: !isPublic },
        },
      });
      toast.success(
        !isPublic
          ? "Your MCP server is now public"
          : "Your MCP server is now private"
      );
    };

    const toggle = (
      <Stack
        align="center"
        gap={1}
        className="border rounded-md p-1 w-fit bg-background"
      >
        <Button
          variant="tertiary"
          size="sm"
          className={cn(
            classes.both,
            isPublic ? classes.active : classes.inactive
          )}
          {...(!isPublic ? { onClick: onToggle } : {})}
        >
          <Button.LeftIcon>
            <Icon name="globe" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Public</Button.Text>
        </Button>
        <Button
          variant="tertiary"
          size="sm"
          className={cn(
            classes.both,
            !isPublic ? classes.active : classes.inactive
          )}
          {...(isPublic ? { onClick: onToggle } : {})}
        >
          <Button.LeftIcon>
            <Icon name="lock" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Private</Button.Text>
        </Button>
      </Stack>
    );

    return (
      <Stack direction="horizontal" align="center" gap={3} className="mt-2">
        {toggle}
        <Stack className="gap-4.5">
          <Type
            small
            className={cn(isPublic ? classes.activeText : classes.inactiveText)}
          >
            Anyone with the URL can read the tools hosted by this server.
            Authentication is still required to use the tools.
          </Type>
          <Type
            small
            className={cn(isPublic ? classes.inactiveText : classes.activeText)}
          >
            Only users with a Gram API Key can read the tools hosted by this
            server.
          </Type>
        </Stack>
      </Stack>
    );
  };

  return (
    <Stack
      className={cn(
        "mb-4",
        !toolset.mcpEnabled && "blur-[2px] pointer-events-none"
      )}
    >
      <PageSection
        heading="Hosted URL"
        description="The URL you or your users will use to access this MCP server."
      >
        <CodeBlock className="mb-2">{mcpUrl ?? ""}</CodeBlock>
        <Block label="Custom Slug" error={mcpSlugError}>
          <BlockInner>
            <Stack direction="horizontal" align="center">
              <Type muted mono variant="small">
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
        <Block label="Custom Domain">
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
        heading="Visibility"
        description="Make your MCP server visible to the world, or protected behind a Gram key."
      >
        <PublicToggle isPublic={mcpIsPublic ?? false} />
      </PageSection>
      <PageSection
        heading="MCP Installation"
        description="Use these configs to connect to this MCP server from a client like
          Cursor or Claude Desktop."
      >
        <Stack className="mt-2" gap={1}>
          <Stack direction="horizontal" align="center" gap={2}>
            <CodeBlock
              copyable={toolset.mcpIsPublic}
            >{`${mcpUrl}/install`}</CodeBlock>
            <Link external to={`${mcpUrl}/install`} noIcon>
              <Button
                variant="secondary"
                className="px-4"
                disabled={!toolset.mcpIsPublic}
              >
                <Button.Text>View</Button.Text>
                <Button.RightIcon>
                  <ExternalLink className="w-4 h-4" />
                </Button.RightIcon>
              </Button>
            </Link>
          </Stack>
          <Type muted small>
            A shareable page for installing your MCP server. Try it in the
            browser!
          </Type>
        </Stack>
        <MCPJson toolset={toolset} />
      </PageSection>
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

function PageSection({
  heading,
  description,
  children,
  className,
}: {
  heading: string;
  description: string;
  fullWidth?: boolean;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <Stack gap={2} className={cn("mb-8", className)}>
      <Heading variant="h3">{heading}</Heading>
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
}: {
  toolset: ToolsetEntry;
  fullWidth?: boolean; // If true, the code block will take up the full width of the page even when there's only one
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
      className="my-4!"
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

  if (!toolset) return { public: "", internal: "" };

  const toolsetTools = tools?.tools?.filter((tool) =>
    toolset.httpTools.some((t) => t.id === tool.id)
  );
  const requiresServerURL = toolsetTools?.some(
    (tool) => !tool.defaultServerUrl
  );

  const envHeaders: string[] = [
    // Security variables (exclude token_url)
    ...(toolset.securityVariables?.flatMap((secVar) =>
      secVar.envVariables.filter(
        (v) => !v.toLowerCase().includes("token_url") // direct token url is always a hidden option right now
      )
    ) ?? []),
    // Server variables (filter server_url unless required)
    ...(toolset.serverVariables?.flatMap((serverVar) =>
      serverVar.envVariables.filter(
        (v) => !v.toLowerCase().includes("server_url") || requiresServerURL
      )
    ) ?? []),
  ];

  // Build the args array for public MCP config
  const mcpJsonPublicArgs = [
    "mcp-remote",
    mcpUrl,
    ...envHeaders.flatMap((header) => [
      "--header",
      `MCP-${header.replace(/_/g, "-")}:${"${VALUE}"}`,
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
        "mcp-remote",
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
  currentSlug?: string
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

  // Check if there are multiple OAuth2 authorization_code security variables
  const oauth2AuthCodeCount =
    toolset.securityVariables?.filter(
      (secVar) =>
        secVar.type === "oauth2" &&
        secVar.oauthTypes?.includes("authorization_code")
    ).length ?? 0;

  const hasMultipleOAuth2AuthCode = oauth2AuthCodeCount > 1;
  const queryClient = useQueryClient();

  const handleBookMeeting = () => {
    telemetry.capture("feature_requested", {
      action: "mcp_oauth_integration",
      toolset_slug: toolsetSlug,
    });
    window.open(
      "https://calendly.com/d/crtj-3tk-wpd/demo-with-speakeasy",
      "_blank"
    );
  };

  const addExternalOAuthMutation = useAddExternalOAuthServerMutation({
    onSuccess: () => {
      // Invalidate both the specific toolset and all toolsets
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
        error instanceof Error ? error.message : "Failed to configure OAuth"
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
      (endpoint) => !parsedMetadata[endpoint]
    );

    if (missingEndpoints.length > 0) {
      setJsonError(
        `Missing required endpoints: ${missingEndpoints.join(", ")}`
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
                    Not Supported: This MCP server has {oauth2AuthCodeCount}{" "}
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
                      <Type className="text-red-500 text-sm mt-1 !text-red-500">
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

            <TabsContent value="proxy" className="space-y-4">
              <div>
                <Type className="font-medium mb-2">OAuth Proxy</Type>
                <Type muted small>
                  Gram can help you get started with an OAuth proxy when you
                  don't fit the very specific MCP OAuth requirements. Book a
                  meeting and we'll help you get started.
                </Type>
                <div className="mt-6 flex gap-3 justify-end items-center">
                  <Button variant="secondary"
                    onClick={() =>
                      window.open(
                        "https://docs.getgram.ai/host-mcp/adding-oauth#oauth-proxy",
                        "_blank"
                      )
                    }
                  >
                    View Docs
                  </Button>
                  <Button onClick={handleBookMeeting}>Book Meeting</Button>
                </div>
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
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}

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

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-w-2xl max-h-[80vh] flex flex-col">
        <Dialog.Header className="flex-shrink-0">
          <Dialog.Title>
            {toolset.externalOauthServer
              ? "External OAuth Configuration"
              : "OAuth Proxy Configuration"}
          </Dialog.Title>
        </Dialog.Header>
        <div className="flex-1 overflow-y-auto">
          <Stack gap={4}>
            {toolset.oauthProxyServer && (
              <div className="flex items-center justify-between">
                <Type className="font-medium">OAuth Proxy Server</Type>
                <Button variant="tertiary"
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
            )}
            {toolset.oauthProxyServer?.oauthProxyProviders?.map((provider) => (
              <Stack key={provider.id} gap={2}>
                <Stack gap={2} className="pl-4">
                  <div>
                    <Type small className="font-medium text-muted-foreground">
                      Authorization Endpoint:
                    </Type>
                    <CodeBlock className="mt-1">
                      {provider.authorizationEndpoint}
                    </CodeBlock>
                  </div>
                  <div>
                    <Type small className="font-medium text-muted-foreground">
                      Token Endpoint:
                    </Type>
                    <CodeBlock className="mt-1">
                      {provider.tokenEndpoint}
                    </CodeBlock>
                  </div>
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
                  {provider.grantTypesSupported &&
                    provider.grantTypesSupported.length > 0 && (
                      <div>
                        <Type
                          small
                          className="font-medium text-muted-foreground"
                        >
                          Supported Grant Types:
                        </Type>
                        <CodeBlock className="mt-1">
                          {provider.grantTypesSupported.join(", ")}
                        </CodeBlock>
                      </div>
                    )}
                </Stack>
              </Stack>
            ))}
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
                        2
                      )}
                    </CodeBlock>
                  </div>
                </Stack>
              </Stack>
            )}
          </Stack>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
