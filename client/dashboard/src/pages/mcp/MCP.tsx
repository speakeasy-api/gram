import { CopyableSlug } from "@/components/name-and-slug";
import { Page } from "@/components/page-layout";
import {
  ToolsetPromptsBadge,
  ToolsetToolsBadge,
} from "@/components/tools-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, Cards } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { CopyButton } from "@/components/ui/copy-button";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useOrganization, useProject } from "@/contexts/Auth";
import { cn, getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { CustomDomain, Toolset } from "@gram/client/models/components";
import {
  useGetDomain,
  useUpdateToolsetMutation,
} from "@gram/client/react-query/index.js";
import { Stack } from "@speakeasy-api/moonshine";
import { Check, Lock, Pencil } from "lucide-react";
import React, { useState } from "react";
import { useToolsets } from "../toolsets/Toolsets";

export default function MCP() {
  const routes = useRoutes();
  const toolsets = useToolsets();
  const domain = useGetDomain(undefined, undefined, {
    refetchOnWindowFocus: false,
    retry: false,
  });

  const content =
    toolsets.length === 0 ? (
      <Stack gap={2}>
        <Heading variant="h3" className="normal-case">
          Expose any toolset as a hosted MCP server in seconds.
        </Heading>
        <Type>
          Head to the{" "}
          <routes.playground.Link>
            <Button size="inline" icon="arrow-right" iconAfter>
              Playground
            </Button>
          </routes.playground.Link>{" "}
          to get started
        </Type>
      </Stack>
    ) : (
      <Cards>
        {toolsets.map((toolset: Toolset) => (
          <McpToolsetCard
            key={toolset.id}
            toolset={toolset}
            domain={domain.error ? undefined : domain.data}
            onUpdate={toolsets.refetch}
          />
        ))}
      </Cards>
    );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <div className="mb-8">
          <Heading variant="h2">Hosted MCP Servers</Heading>
          <Type className="text-muted-foreground mt-2">
            Use any Gram toolset as a hosted MCP server.
          </Type>
        </div>
        {content}
      </Page.Body>
    </Page>
  );
}

export function McpToolsetCard({
  toolset,
  domain,
  onUpdate,
}: {
  toolset: Toolset;
  domain?: CustomDomain;
  onUpdate: () => void;
}) {
  const routes = useRoutes();
  const project = useProject();
  const organization = useOrganization();
  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: onUpdate,
  });

  const [mcpModalOpen, setMcpModalOpen] = useState(false);
  const [publishModalOpen, setPublishModalOpen] = useState(false);
  const [mcpSlug, setMcpSlug] = useState("");
  const [mcpIsPublic, setMcpIsPublic] = useState(false);
  const [mcpSlugError, setMcpSlugError] = useState<string | null>(null);

  // Determine which server URL to use
  let customServerURL: string | undefined;
  if (domain && toolset.customDomainId && domain.id == toolset.customDomainId) {
    customServerURL = `https://${domain.domain}`;
  }

  // Sync modal state with toolset when opening
  React.useEffect(() => {
    if (publishModalOpen && toolset) {
      if (toolset.mcpSlug && toolset.mcpSlug.length > 0) {
        setMcpSlug(toolset.mcpSlug);
      } else {
        const chars = "abcdefghijklmnopqrstuvwxyz0123456789";
        let rand = "";
        for (let i = 0; i < 5; i++) {
          rand += chars.charAt(Math.floor(Math.random() * chars.length));
        }
        setMcpSlug(organization.slug + "-" + project.slug + "-" + rand);
      }
      setMcpIsPublic(!!toolset.mcpIsPublic);
      setMcpSlugError(null);
    }
  }, [publishModalOpen, toolset, organization, project, domain]);

  let mcpJsonPublic: string | undefined = undefined;
  const envHeaders =
    toolset.relevantEnvironmentVariables?.filter(
      (v) => !v.toLowerCase().includes("server_url")
    ) ?? [];

  const urlSuffix = toolset.mcpSlug
    ? toolset.mcpSlug
    : `${project.slug}/${toolset.slug}/${toolset.defaultEnvironmentSlug}`;
  const mcpUrl = `${
    toolset.mcpSlug && customServerURL ? customServerURL : getServerURL()
  }/mcp/${urlSuffix}`;

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
  mcpJsonPublic = `{
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

  // MCP Slug validation
  const validateMcpSlug = (slug: string) => {
    if (!slug) return "MCP Slug is required";
    if (slug.length > 40) return "Must be 40 characters or less";
    if (!/^[a-z0-9_-]+$/.test(slug))
      return "Lowercase letters, numbers, _ or - only";
    return null;
  };

  const handleMcpSlugChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    let value = e.target.value;
    value = value.slice(0, 40);
    setMcpSlug(value);
    setMcpSlugError(validateMcpSlug(value));
  };

  const publishDialog = (
    <Dialog open={publishModalOpen} onOpenChange={setPublishModalOpen}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>MCP Settings</Dialog.Title>
          <Dialog.Description>
            Set a shorted MCP Slug and visibility setting for this server.
          </Dialog.Description>
        </Dialog.Header>
        <Stack gap={6} className="my-4">
          <div>
            <label className="block mb-1 font-medium">MCP Slug</label>
            <Stack direction="horizontal" align="start">
              <Type muted mono variant="small" className="mt-2">
                {toolset.mcpSlug && customServerURL
                  ? `${customServerURL}/mcp/`
                  : `${getServerURL()}/mcp/`}
              </Type>
              <div className="w-full">
                <input
                  className="border rounded px-2 py-1 w-full"
                  placeholder="Enter MCP Slug"
                  value={mcpSlug}
                  onChange={handleMcpSlugChange}
                  maxLength={40}
                />
                <div className="text-destructive text-xs mt-1 h-1">
                  {mcpSlugError}
                </div>
              </div>
            </Stack>
          </div>
          <div className="flex items-center gap-2">
            <Checkbox
              checked={mcpIsPublic}
              onCheckedChange={(checked) => setMcpIsPublic(!!checked)}
              id={`mcp-public-checkbox-${toolset.slug}`}
            />
            <label
              htmlFor={`mcp-public-checkbox-${toolset.slug}`}
              className="font-medium select-none cursor-pointer"
            >
              Public
            </label>
          </div>
        </Stack>
        <Dialog.Footer>
          <Button variant="ghost" onClick={() => setPublishModalOpen(false)}>
            Cancel
          </Button>
          <Button
            onClick={() => {
              updateToolsetMutation.mutate({
                request: {
                  slug: toolset.slug,
                  updateToolsetRequestBody: {
                    mcpSlug: mcpSlug || undefined,
                    mcpIsPublic,
                  },
                },
              });
              setPublishModalOpen(false);
            }}
            disabled={!!mcpSlugError || !mcpSlug}
          >
            Save
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );

  const mcpConfigDialog = (
    <Dialog open={mcpModalOpen} onOpenChange={setMcpModalOpen}>
      <Dialog.Content className="!max-w-3xl !p-10">
        <Dialog.Header>
          <Dialog.Title>MCP Config</Dialog.Title>
        </Dialog.Header>
        <div className="flex flex-col gap-8 max-h-96 overflow-auto">
          {toolset.mcpIsPublic && mcpJsonPublic && (
            <div>
              <div className="font-semibold">Public Server</div>
              <div className="relative bg-muted p-3 rounded-md">
                <CopyButton text={mcpJsonPublic} absolute />
                <pre className="break-all whitespace-pre-wrap text-xs pr-10">
                  {mcpJsonPublic}
                </pre>
              </div>
            </div>
          )}
          <div>
            <div className="font-semibold">
              Authenticated Server (with Gram key)
            </div>
            <div className="relative bg-muted p-3 rounded-md">
              <CopyButton text={mcpJsonInternal} absolute />
              <pre className="break-all whitespace-pre-wrap text-xs pr-10">
                {mcpJsonInternal}
              </pre>
            </div>
          </div>
        </div>
        <div className="flex justify-end mt-4">
          <Button onClick={() => setMcpModalOpen(false)}>Close</Button>
        </div>
      </Dialog.Content>
    </Dialog>
  );

  const badges = (
    <Stack direction="horizontal" gap={2} align="center">
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge
              size="md"
              variant="outline"
              className={"flex items-center gap-1"}
            >
              {toolset.mcpIsPublic ? (
                <Check className={cn("w-4 h-4 stroke-3", "text-green-500")} />
              ) : (
                <Lock className={cn("w-4 h-4", "text-orange-500")} />
              )}
              {toolset.mcpIsPublic ? "Public" : "Private"}
            </Badge>
          </TooltipTrigger>
          <TooltipContent>
            {toolset.mcpIsPublic
              ? `Your MCP server is publicly reachable at ${mcpUrl}`
              : "Your MCP server can be used alongisde a Gram API key."}
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <ToolsetPromptsBadge toolset={toolset} variant="outline" />
      <ToolsetToolsBadge toolset={toolset} variant="outline" />
    </Stack>
  );

  return (
    <Card>
      <Card.Header>
        <Stack direction="horizontal" gap={2} justify={"space-between"}>
          <Card.Title>
            <CopyableSlug slug={toolset.slug}>
              <routes.toolsets.toolset.Link params={[toolset.slug]}>
                {toolset.name}
              </routes.toolsets.toolset.Link>
            </CopyableSlug>
          </Card.Title>
          {badges}
        </Stack>
        <Card.Description>
          <Stack direction="horizontal" className="group" align="center">
            <Type muted mono small className="break-all text-xs">
              {mcpUrl}
            </Type>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setPublishModalOpen(true)}
              className="group-hover:opacity-100 opacity-0 transition-opacity ml-1"
            >
              <Pencil className="h-4 w-4" />
            </Button>
            <CopyButton
              text={mcpUrl}
              size="icon-sm"
              className="group-hover:opacity-100 opacity-0 transition-opacity"
            />
          </Stack>
        </Card.Description>
      </Card.Header>
      <Card.Content></Card.Content>
      <Card.Footer>
        <div className="flex w-full items-center">
          {domain && !toolset.customDomainId && !!toolset.mcpSlug && (
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
          )}
          <Button
            icon="braces"
            className="ml-auto"
            onClick={() => setMcpModalOpen(true)}
          >
            MCP Config
          </Button>
          <Button
            icon="package"
            className="ml-2"
            onClick={() => setPublishModalOpen(true)}
          >
            {toolset.mcpIsPublic ? "Publish Settings" : "Publish"}
          </Button>
        </div>
        {mcpConfigDialog}
        {publishDialog}
      </Card.Footer>
    </Card>
  );
}
