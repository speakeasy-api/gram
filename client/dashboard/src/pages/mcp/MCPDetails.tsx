import { CodeBlock } from "@/components/code";
import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Link } from "@/components/ui/link";
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
import { Toolset, ToolsetEntry } from "@gram/client/models/components";
import {
  invalidateAllToolset,
  useGetDomain,
  useListTools,
  useToolsetSuspense,
  useUpdateToolsetMutation,
} from "@gram/client/react-query";
import { Grid, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Globe } from "lucide-react";
import React, { useEffect, useState } from "react";
import { Outlet, useParams } from "react-router";
import { toast } from "sonner";
import { Block, BlockInner } from "../toolBuilder/components";
import { ToolsetCard } from "../toolsets/ToolsetCard";
import { onboardingStepStorageKeys } from "../home/Home";

export function MCPDetailsRoot() {
  return <Outlet />;
}

export function MCPDetailPage() {
  const { toolsetSlug } = useParams();

  const toolset = useToolsetSuspense({ slug: toolsetSlug! });
  const showOAuthButton = toolset.data.securityVariables?.some(secVar => 
    secVar.type === "oauth2" && 
    secVar.oauthTypes?.includes("authorization_code")
  ) ?? false;
  const isOAuthConnected = !!(toolset.data.oauthProxyServer || toolset.data.externalOauthServer);
  const [isOAuthModalOpen, setIsOAuthModalOpen] = useState(false);
  const [isOAuthDetailsModalOpen, setIsOAuthDetailsModalOpen] = useState(false);

  useEffect(() => {
    localStorage.setItem(onboardingStepStorageKeys.configure, "true");
  }, []);

  return (
    <Stack>
      <Stack direction="horizontal" align="center" className="mb-8 justify-between">
        <Heading variant="h2">
          MCP Details
        </Heading>
        {showOAuthButton && (
          <Button 
            variant="secondary"
            size="lg"
            onClick={() => isOAuthConnected ? setIsOAuthDetailsModalOpen(true) : setIsOAuthModalOpen(true)}
          >
            {isOAuthConnected ? "OAuth Connected" : "Connect OAuth"}
          </Button>
        )}
      </Stack>
      <PageSection
        heading="Source Toolset"
        description="MCP servers expose the contents of a single toolset. To change the
          tools or prompts exposed by this MCP server, update the source toolset
          below."
      >
        <ToolsetCard toolset={toolset.data} className="max-w-2xl" />
      </PageSection>
      <MCPDetails toolset={toolset.data} />
      <ConnectOAuthModal
        isOpen={isOAuthModalOpen}
        onClose={() => setIsOAuthModalOpen(false)}
        toolsetSlug={toolset.data.slug}
      />
      <OAuthDetailsModal
        isOpen={isOAuthDetailsModalOpen}
        onClose={() => setIsOAuthDetailsModalOpen(false)}
        toolset={toolset.data}
      />
    </Stack>
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
  const { orgSlug, projectSlug } = useParams();
  const { domain } = useCustomDomain();

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
          <Button
            variant="outline"
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
      <Button
        variant="outline"
        size="sm"
        onClick={() => {
          if (session.gramAccountType == "free") {
            setIsCustomDomainModalOpen(true);
          } else {
            window.location.href = `/${orgSlug}/${projectSlug}/settings`;
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
    <Button
      variant="ghost"
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
          icon={"globe"}
          variant="ghost"
          size="sm"
          className={cn(
            classes.both,
            isPublic ? classes.active : classes.inactive
          )}
          {...(!isPublic ? { onClick: onToggle } : {})}
        >
          Public
        </Button>
        <Button
          icon={"lock"}
          variant="ghost"
          size="sm"
          className={cn(
            classes.both,
            !isPublic ? classes.active : classes.inactive
          )}
          {...(isPublic ? { onClick: onToggle } : {})}
        >
          Private
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
    <Stack className="mb-4">
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
                variant="outline"
                className="px-4"
                icon={"external-link"}
                iconAfter
                disabled={!toolset.mcpIsPublic}
                tooltip={
                  toolset.mcpIsPublic
                    ? "Open the install page"
                    : "Make your MCP server public to view the installation page."
                }
              >
                View
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
    ...(toolset.securityVariables?.flatMap(secVar => 
      secVar.envVariables.filter(v => 
        !v.toLowerCase().includes("token_url") // direct token url is always a hidden option right now
      )
    ) ?? []),
    // Server variables (filter server_url unless required)
    ...(toolset.serverVariables?.flatMap(serverVar => 
      serverVar.envVariables.filter(v => 
        !v.toLowerCase().includes("server_url") || requiresServerURL
      )
    ) ?? [])
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
}: {
  isOpen: boolean;
  onClose: () => void;
  toolsetSlug: string;
}) {
  const session = useSession();
  const isAccountUpgrade = session.gramAccountType === "free";

  return (
    <FeatureRequestModal
      isOpen={isOpen}
      onClose={onClose}
      title="Connect OAuth"
      description={
        isAccountUpgrade 
          ? "A Managed OAuth integration requires upgrading to a pro account type. Someone should be in touch shortly, or feel free to book a meeting directly."
          : "Gram can help you connect an OAuth provider directly to your MCP server. Book a meeting and we'll help you get started."
      }
      actionType="mcp_oauth_integration"
      icon={Globe}
      telemetryData={{ slug: toolsetSlug }}
      docsLink="https://docs.getgram.ai/build-mcp/adding-oauth"
      accountUpgrade={isAccountUpgrade}
    />
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

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-w-2xl">
        <Dialog.Header>
          <Dialog.Title>{toolset.externalOauthServer ? "External OAuth Configuration" : "OAuth Proxy Configuration"}</Dialog.Title>
        </Dialog.Header>
        <Stack gap={4}>
          {toolset.oauthProxyServer?.oauthProxyProviders?.map((provider) => (
            <Stack key={provider.id} gap={2}>
              <Stack gap={2} className="pl-4">
                <div>
                  <Type small className="font-medium text-muted-foreground">Authorization Endpoint:</Type>
                  <CodeBlock className="mt-1">{provider.authorizationEndpoint}</CodeBlock>
                </div>
                <div>
                  <Type small className="font-medium text-muted-foreground">Token Endpoint:</Type>
                  <CodeBlock className="mt-1">{provider.tokenEndpoint}</CodeBlock>
                </div>
                {provider.scopesSupported && provider.scopesSupported.length > 0 && (
                  <div>
                    <Type small className="font-medium text-muted-foreground">Supported Scopes:</Type>
                    <CodeBlock className="mt-1">{provider.scopesSupported.join(", ")}</CodeBlock>
                  </div>
                )}
                {provider.grantTypesSupported && provider.grantTypesSupported.length > 0 && (
                  <div>
                    <Type small className="font-medium text-muted-foreground">Supported Grant Types:</Type>
                    <CodeBlock className="mt-1">{provider.grantTypesSupported.join(", ")}</CodeBlock>
                  </div>
                )}
              </Stack>
            </Stack>
          ))}
          {toolset.externalOauthServer && (
            <Stack gap={2}>
              <Stack gap={2} className="pl-4">
                <div>
                  <Type small className="font-medium text-muted-foreground">OAuth Authorization Server Metadata:</Type>
                  <CodeBlock className="mt-1">
                    {mcpUrl ? `${new URL(mcpUrl).origin}/.well-known/oauth-authorization-server/mcp/${toolset.mcpSlug}` : ""}
                  </CodeBlock>
                </div>
              </Stack>
            </Stack>
          )}
        </Stack>
      </Dialog.Content>
    </Dialog>
  );
}
