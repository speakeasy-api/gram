import { CodeBlock } from "@/components/code";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
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
import { Toolset } from "@gram/client/models/components";
import {
  invalidateAllToolset,
  useGetDomain,
  useToolsetSuspense,
  useUpdateToolsetMutation,
} from "@gram/client/react-query";
import { Grid, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import React, { useEffect, useState } from "react";
import { Outlet, useParams } from "react-router";
import { toast } from "sonner";
import { Block, BlockInner } from "../toolBuilder/components";
import { ToolsetCard } from "../toolsets/ToolsetCard";

export function MCPDetailsRoot() {
  return <Outlet />;
}

export function MCPDetailPage() {
  const { toolsetSlug } = useParams();

  const toolset = useToolsetSuspense({ slug: toolsetSlug! });

  return (
    <Stack>
      <Heading variant="h2" className="mb-8">
        MCP Details
      </Heading>
      <PageSection
        heading="Source Toolset"
        description="MCP servers expose the contents of a single toolset. To change the
          tools or prompts exposed by this MCP server, update the source toolset
          below."
      >
        <ToolsetCard toolset={toolset.data} className="m-0!" />
      </PageSection>
      <MCPDetails toolset={toolset.data} />
    </Stack>
  );
}

export function useMcpUrl(toolset: Toolset | undefined) {
  const { data: domain } = useGetDomain(undefined, undefined, {
    refetchOnWindowFocus: false,
    retry: false,
  });
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
  const routes = useRoutes();

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => invalidateAllToolset(queryClient),
  });

  const { data: domain } = useGetDomain(undefined, undefined, {
    refetchOnWindowFocus: false,
    retry: false,
  });

  const [mcpSlug, setMcpSlug] = useState(toolset.mcpSlug || "");
  const [mcpIsPublic, setMcpIsPublic] = useState(toolset.mcpIsPublic);

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
          if (session.gramAccountType === "free") {
            telemetry.capture("feature_requested", {
              action: "mcp_custom_domain",
              slug: toolset.slug,
            });
            alert(
              "Custom domains for your account require approval by the Speakeasy team. Someone should be in touch shortly, or feel free to reach out directly."
            );
          } else {
            window.location.href = `/${orgSlug}/${projectSlug}/settings`;
          }
          alert(
            "Custom domains require approval by the Speakeasy team. Someone should be in touch shortly, or feel free to reach out directly."
          );
        }}
      >
        Configure
      </Button>
    );

  const publicToggle = (
    <div className="flex items-center gap-2 border-1 rounded-sm px-2 py-1">
      <Checkbox
        checked={mcpIsPublic}
        onCheckedChange={(checked) => setMcpIsPublic(!!checked)}
        id={`mcp-public-checkbox-${toolset.slug}`}
      />
      <label
        htmlFor={`mcp-public-checkbox-${toolset.slug}`}
        className="font-medium select-none cursor-pointer"
      >
        <Type small>Public</Type>
      </label>
    </div>
  );

  const anyChanges =
    mcpIsPublic !== toolset.mcpIsPublic || mcpSlug !== toolset.mcpSlug;

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
        toast.success("MCP settings saved successfully");

        telemetry.capture("mcp_event", {
          action: "mcp_settings_saved",
          slug: toolset.slug,
          isPublic: mcpIsPublic,
        });
      }}
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
      className="mr-2"
    >
      Discard
    </Button>
  );

  return (
    <Stack className="mb-4">
      <PageSection
        heading="Hosted URL"
        headingRHS={publicToggle}
        description="The URL you or your users will use to access this MCP server."
        className="max-w-2xl mb-2"
      >
        <CodeBlock className="mb-2">{mcpUrl ?? ""}</CodeBlock>
        <Block label="Custom Slug" className="max-w-2xl" error={mcpSlugError}>
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
        <div className="ml-auto">
          {discardButton}
          {saveButton}
        </div>
      </PageSection>
      {toolset.mcpIsPublic && (
        <PageSection
          heading="MCP Installation"
          description="A simple hosted page for installing your MCP server"
        >
          <Stack direction="horizontal" align="center" gap={2}>
            <routes.mcp.details.hosted_page.Link params={[toolset.slug]}>
              <Button variant="outline" size="sm" className="px-8">
                View
              </Button>
            </routes.mcp.details.hosted_page.Link>
            <CodeBlock className="max-w-3xl">{`${mcpUrl}/page`}</CodeBlock>
          </Stack>
        </PageSection>
      )}
      <PageSection
        heading="MCP Config"
        description="Use this config to connect to this MCP server from a client like
          Cursor or Claude Desktop."
      >
        <MCPJson toolset={toolset} />
      </PageSection>
    </Stack>
  );
}

function PageSection({
  heading,
  headingRHS,
  description,
  children,
  className,
}: {
  heading: string;
  headingRHS?: React.ReactNode;
  description: string;
  fullWidth?: boolean;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <Stack gap={2} className={cn("mb-8", className)}>
      <Stack direction="horizontal" align="center" justify="space-between">
        <Heading variant="h3">{heading}</Heading>
        {headingRHS}
      </Stack>
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
  toolset: Toolset;
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

  const publicSettingsJson =
    toolset.mcpIsPublic &&
    mcpJsonPublic &&
    ((
      <Grid.Item>
        <Type className="font-medium">Public Server</Type>
        <CodeBlock onCopy={onCopy}>{mcpJsonPublic}</CodeBlock>
      </Grid.Item> // This any is necessary because the Grid API is a bit messed up and doesn't accept null elements 
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ) as any);

  return (
    <Grid
      gap={4}
      columns={
        publicSettingsJson || !fullWidth
          ? { xs: 1, md: 2, lg: 2, xl: 2, "2xl": 2 }
          : 1
      }
    >
      {publicSettingsJson}
      <Grid.Item>
        <Type className="font-medium">
          Authenticated Server{" "}
          <span className="text-muted-foreground font-normal">
            (with Gram key)
          </span>
        </Type>
        <CodeBlock onCopy={onCopy}>{mcpJsonInternal}</CodeBlock>
      </Grid.Item>
    </Grid>
  );
}

export const useMcpConfigs = (toolset: Toolset | undefined) => {
  const { url: mcpUrl } = useMcpUrl(toolset);

  if (!toolset) return { public: "", internal: "" };

  const envHeaders =
    toolset.relevantEnvironmentVariables?.filter(
      (v) => !v.toLowerCase().includes("server_url")
    ) ?? [];

  // Build the args array for public MCP config
  const mcpJsonPublicArgs = [
    "mcp-remote",
    mcpUrl,
    ...envHeaders.flatMap((header) => [
      "--header",
      `MCP-${header.replace(/_/g, "-")}:${"${VALUE}"}`,
    ]),
  ];
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
      "args": ${argsStringIndented}
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
        "Authorization: \${GRAM_KEY}"
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
    if (slug.length > 40) return "Must be 40 characters or less";
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
