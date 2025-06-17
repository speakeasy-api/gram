import { CopyableSlug } from "@/components/name-and-slug";
import { Page } from "@/components/page-layout";
import {
  ToolsetPromptsBadge,
  ToolsetToolsBadge,
} from "@/components/tools-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, Cards } from "@/components/ui/card";
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
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Toolset } from "@gram/client/models/components";
import { Stack } from "@speakeasy-api/moonshine";
import { Check, Lock, Pencil } from "lucide-react";
import { useState } from "react";
import { Outlet } from "react-router";
import { useToolsets } from "../toolsets/Toolsets";
import { MCPJson, useMcpUrl } from "./MCPDetails";

export function MCPRoot() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Outlet />
      </Page.Body>
    </Page>
  );
}

export function MCPOverview() {
  const routes = useRoutes();
  const toolsets = useToolsets();

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
          <McpToolsetCard key={toolset.id} toolset={toolset} />
        ))}
      </Cards>
    );

  return (
    <>
      <div className="mb-8">
        <Heading variant="h2">Hosted MCP Servers</Heading>
        <Type className="text-muted-foreground mt-2">
          Access any of your Gram toolsets below as a hosted MCP server
        </Type>
      </div>
      {content}
    </>
  );
}

export function McpToolsetCard({ toolset }: { toolset: Toolset }) {
  const routes = useRoutes();

  const { url: mcpUrl } = useMcpUrl(toolset);

  const [mcpModalOpen, setMcpModalOpen] = useState(false);

  const mcpConfigDialog = (
    <Dialog open={mcpModalOpen} onOpenChange={setMcpModalOpen}>
      <Dialog.Content className="!max-w-3xl !p-10">
        <Dialog.Header>
          <Dialog.Title>MCP Config</Dialog.Title>
        </Dialog.Header>
        <MCPJson toolset={toolset} fullWidth />
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
              <routes.mcp.details.Link params={[toolset.slug]}>
                {toolset.name}
              </routes.mcp.details.Link>
            </CopyableSlug>
          </Card.Title>
          {badges}
        </Stack>
        <Card.Description>
          <Stack direction="horizontal" className="group" align="center">
            <Type muted mono small className="break-all text-xs">
              {mcpUrl}
            </Type>
            <routes.mcp.details.Link params={[toolset.slug]}>
              <Button
                variant="ghost"
                size="icon-sm"
                className="group-hover:opacity-100 opacity-0 transition-opacity ml-1"
              >
                <Pencil className="h-4 w-4" />
              </Button>
            </routes.mcp.details.Link>
            <CopyButton
              text={mcpUrl}
              size="icon-sm"
              className="group-hover:opacity-100 opacity-0 transition-opacity"
            />
          </Stack>
        </Card.Description>
      </Card.Header>
      <Card.Footer>
        <Stack direction="horizontal" gap={2} align="center">
          <Button
            icon="braces"
            className="ml-auto"
            onClick={() => setMcpModalOpen(true)}
            variant="secondary"
          >
            View/Copy Config
          </Button>
          <routes.mcp.details.Link params={[toolset.slug]}>
            <Button icon="settings">Edit</Button>
          </routes.mcp.details.Link>
        </Stack>
        {mcpConfigDialog}
      </Card.Footer>
    </Card>
  );
}
