import { Page } from "@/components/page-layout";
import { ServerCard } from "@/components/server-card";
import { Button } from "@speakeasy-api/moonshine";
import { Dialog } from "@/components/ui/dialog";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { useState } from "react";
import { Outlet } from "react-router";
import { useToolsets } from "../toolsets/Toolsets";
import { MCPJson } from "./MCPDetails";
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
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          {toolsets.map((toolset) => (
            <McpToolsetCard key={toolset.id} toolset={toolset} />
          ))}
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}

export function McpToolsetCard({ toolset }: { toolset: ToolsetForMCP }) {
  const routes = useRoutes();
  const [mcpModalOpen, setMcpModalOpen] = useState(false);

  return (
    <>
      <ServerCard
        toolset={toolset}
        onCardClick={() => routes.mcp.details.goTo(toolset.slug)}
        additionalActions={[
          {
            label: "View/Copy Config",
            onClick: () => setMcpModalOpen(true),
            icon: "braces",
          },
        ]}
      />
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
    </>
  );
}
