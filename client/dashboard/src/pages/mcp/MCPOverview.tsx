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
import { MoreActions } from "@/components/ui/more-actions";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { Stack } from "@speakeasy-api/moonshine";
import { Check, Lock, Pencil } from "lucide-react";
import { useState } from "react";
import { Outlet } from "react-router";
import { useToolsets } from "../toolsets/Toolsets";
import { MCPJson, useMcpUrl } from "./MCPDetails";
import { MCPEmptyState } from "./MCPEmptyState";

// Define specific type for MCP components
type ToolsetForMCP = ToolsetEntry;

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
  const toolsets = useToolsets();

  if (!toolsets.isLoading && toolsets.length === 0) {
    return <MCPEmptyState />;
  }

  return (
    <Page.Section>
      <Page.Section.Title>Hosted MCP Servers</Page.Section.Title>
      <Page.Section.Description>
        Access any of your Gram toolsets below as a hosted MCP server
      </Page.Section.Description>
      <Page.Section.Body>
        <Cards>
          {toolsets.map((toolset) => (
            <McpToolsetCard key={toolset.id} toolset={toolset} />
          ))}
        </Cards>
      </Page.Section.Body>
    </Page.Section>
  );
}

export function McpToolsetCard({ toolset }: { toolset: ToolsetForMCP }) {
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
      <ToolsetToolsBadge toolset={toolset} variant="outline" />
      <ToolsetPromptsBadge toolset={toolset} variant="outline" />
    </Stack>
  );

  return (
    <routes.mcp.details.Link
      params={[toolset.slug]}
      className="hover:no-underline"
    >
      <Card>
        <Card.Header>
          <Card.Title>
            <CopyableSlug slug={toolset.slug}>{toolset.name}</CopyableSlug>
          </Card.Title>
          <MoreActions
            actions={[
              {
                label: "View/Copy Config",
                onClick: () => setMcpModalOpen(true),
                icon: "braces",
              },
            ]}
          />
        </Card.Header>
        <Card.Content>
          <Card.Description className="mt-[-6px]">
            <Stack direction="horizontal" className="group" align="center">
              <Type muted mono small className="break-all text-xs">
                {mcpUrl}
              </Type>
              <Button
                variant="ghost"
                size="icon-sm"
                className="group-hover:opacity-100 opacity-0 transition-opacity ml-1"
              >
                <Pencil className="h-4 w-4" />
              </Button>
              <CopyButton
                text={mcpUrl ?? ""}
                size="icon-sm"
                className="group-hover:opacity-100 opacity-0 transition-opacity"
              />
            </Stack>
          </Card.Description>
        </Card.Content>
        <Card.Footer>
          {badges}
          <div />
        </Card.Footer>
        {mcpConfigDialog}
      </Card>
    </routes.mcp.details.Link>
  );
}
