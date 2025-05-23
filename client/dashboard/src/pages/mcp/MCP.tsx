import React, { useState } from "react";
import { NameAndSlug } from "@/components/name-and-slug";
import { Page } from "@/components/page-layout";
import { ToolsBadge } from "@/components/tools-badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Toolset } from "@gram/client/models/components";
import { useListToolsetsSuspense } from "@gram/client/react-query/index.js";
import { Stack } from "@speakeasy-api/moonshine";
import { CheckCircle2, Copy, Check, AlertTriangle } from "lucide-react";
import { useOrganization, useProject } from "@/contexts/Auth";
import { Badge } from "@/components/ui/badge";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
  TooltipProvider,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { Checkbox } from "@/components/ui/checkbox";
import { useUpdateToolsetMutation } from "@gram/client/react-query/index.js";

function useToolsets() {
  const { data: toolsets, refetch } = useListToolsetsSuspense();
  return Object.assign(toolsets.toolsets, { refetch });
}

export default function MCP() {
  const toolsets = useToolsets();

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
        {toolsets.map((toolset: Toolset) => (
          <McpToolsetCard key={toolset.id} toolset={toolset} />
        ))}
      </Page.Body>
    </Page>
  );
}

function McpToolsetCard({ toolset }: { toolset: Toolset }) {
  const routes = useRoutes();
  const [mcpModalOpen, setMcpModalOpen] = useState(false);
  const [isCopied, setIsCopied] = useState(false);
  const [visibilityModalOpen, setVisibilityModalOpen] = useState(false);
  const [mcpSlug, setMcpSlug] = useState("");
  const [mcpIsPublic, setMcpIsPublic] = useState(false);
  const [mcpSlugError, setMcpSlugError] = useState<string | null>(null);
  const project = useProject();
  const organization = useOrganization();
  const toolsets = useToolsets();
  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => toolsets.refetch(),
  });

  // Sync modal state with toolset when opening
  React.useEffect(() => {
    if (visibilityModalOpen && toolset) {
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
  }, [visibilityModalOpen, toolset, organization, project]);

  let mcpJsonPublic: string | undefined = undefined;
  const headerObj = toolset.relevantEnvironmentVariables && toolset.relevantEnvironmentVariables.length > 0
    ? Object.fromEntries(
        toolset.relevantEnvironmentVariables
          .filter(v => !v.toLowerCase().includes('server_url'))
          .map(v => [v, 'VALUE'])
      )
    : {};
  const headerJson = JSON.stringify(headerObj).replace(/"/g, '\\"');
  mcpJsonPublic = `{
    "mcpServers": {
      "Gram${toolset.slug.replace(/-/g, "").replace(/^./, c => c.toUpperCase())}": {
        "command": "npx",
        "args": [
          "mcp-remote",
          "${getServerURL()}/mcp/${toolset.mcpSlug}",
          "--allow-http",
          "--header",
          "MCP-Environment:${"${MCP_ENVIRONMENT}"}"
        ],
        "env": {
          "MCP_ENVIRONMENT": "${headerJson}"
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
          "${getServerURL()}/mcp/${project.slug}/${toolset.slug}/${toolset.defaultEnvironmentSlug}",
          "--allow-http",
          "--header",
          "Authorization:${"${GRAM_KEY}"}"
        ],
        "env": {
          "GRAM_KEY": "Bearer <your-key-here>"
        }
      }
    }
  }`;

  const handleCopy = async (config: string) => {
    await navigator.clipboard.writeText(config);
    setIsCopied(true);
    setTimeout(() => setIsCopied(false), 2000);
  };

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

  return (
    <Card className="mb-6 relative">
      <Card.Header>
        <Stack direction="horizontal" gap={2} justify={"space-between"}>
          <Card.Title>
            <NameAndSlug
              name={toolset.name}
              slug={toolset.slug}
              linkTo={routes.toolsets.toolset.href(toolset.slug)}
            />
          </Card.Title>
          <Stack direction="horizontal" gap={2} align="center">
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge size="md" className={"flex items-center gap-1"}>
                    {toolset.mcpIsPublic ? (
                      <Check
                        className={cn("w-4 h-4 stroke-3", "text-green-500")}
                      />
                    ) : (
                      <AlertTriangle
                        className={cn("w-4 h-4", "text-orange-500")}
                      />
                    )}
                    {toolset.mcpIsPublic ? "Public" : "Private"}
                  </Badge>
                </TooltipTrigger>
                <TooltipContent>
                  {toolset.mcpIsPublic
                    ? `Your MCP server is publicly reachable at ${getServerURL()}/mcp/${
                        toolset.mcpSlug
                      }`
                    : "Your MCP server can be used alongisde a Gram API key."}
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
            <ToolsBadge tools={toolset.httpTools} />
          </Stack>
        </Stack>
      </Card.Header>
      <Card.Content>
        <div className="flex items-center justify-start w-full">
          <Button
            variant="outline"
            className="group"
            onClick={() => setMcpModalOpen(true)}
          >
            MCP Config
          </Button>
          <Dialog open={mcpModalOpen} onOpenChange={setMcpModalOpen}>
            <Dialog.Content className="!max-w-3xl !p-10">
              <Dialog.Header>
                <Dialog.Title></Dialog.Title>
              </Dialog.Header>
              <div className="flex flex-col gap-8 max-h-96 overflow-auto">
                {toolset.mcpIsPublic && mcpJsonPublic && (
                  <div className="relative bg-muted p-3 rounded-md">
                    <div className="font-semibold mb-2">
                      Public Server
                    </div>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => handleCopy(mcpJsonPublic!)}
                      className="absolute top-3 right-3 z-10 bg-background shadow-md border border-border hover:bg-accent"
                      style={{ boxShadow: "0 2px 8px rgba(0,0,0,0.08)" }}
                    >
                      {isCopied ? (
                        <CheckCircle2 className="h-5 w-5 text-green-500" />
                      ) : (
                        <Copy className="h-5 w-5" />
                      )}
                    </Button>
                    <pre className="break-all whitespace-pre-wrap text-xs pr-10">
                      {mcpJsonPublic}
                    </pre>
                  </div>
                )}
                <div className="relative bg-muted p-3 rounded-md">
                  <div className="font-semibold mb-2">
                    Authenticated Server (with Gram key)
                  </div>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() => handleCopy(mcpJsonInternal)}
                    className="absolute top-3 right-3 z-10 bg-background shadow-md border border-border hover:bg-accent"
                    style={{ boxShadow: "0 2px 8px rgba(0,0,0,0.08)" }}
                  >
                    {isCopied ? (
                      <CheckCircle2 className="h-5 w-5 text-green-500" />
                    ) : (
                      <Copy className="h-5 w-5" />
                    )}
                  </Button>
                  <pre className="break-all whitespace-pre-wrap text-xs pr-10">
                    {mcpJsonInternal}
                  </pre>
                </div>
              </div>
              <div className="flex justify-end mt-4">
                <Button onClick={() => setMcpModalOpen(false)}>Close</Button>
              </div>
            </Dialog.Content>
          </Dialog>
        </div>
        <div
          style={{
            position: "absolute",
            bottom: 24,
            right: 24,
            zIndex: 20,
          }}
        >
          <Button
            variant="outline"
            onClick={() => setVisibilityModalOpen(true)}
          >
            {toolset.mcpSlug ? "Edit" : "Add Slug"}
          </Button>
        </div>
        {/* MCP Visibility Modal */}
        <Dialog
          open={visibilityModalOpen}
          onOpenChange={setVisibilityModalOpen}
        >
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
                <input
                  className="border rounded px-2 py-1 w-full"
                  placeholder="Enter MCP Slug"
                  value={mcpSlug}
                  onChange={handleMcpSlugChange}
                  maxLength={40}
                />
                {mcpSlugError && (
                  <div className="text-destructive text-xs mt-1">
                    {mcpSlugError}
                  </div>
                )}
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
              <Button
                variant="ghost"
                onClick={() => setVisibilityModalOpen(false)}
              >
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
                  setVisibilityModalOpen(false);
                }}
                disabled={!!mcpSlugError || !mcpSlug}
              >
                Save
              </Button>
            </Dialog.Footer>
          </Dialog.Content>
        </Dialog>
      </Card.Content>
    </Card>
  );
}
