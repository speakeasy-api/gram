import { Page } from "@/components/page-layout";
import { ServerCard } from "@/components/server-card";
import { Dialog } from "@/components/ui/dialog";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { Button } from "@speakeasy-api/moonshine";
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
  const [configModalOpen, setConfigModalOpen] = useState(false);
  const [configModalToolset, setConfigModalToolset] =
    useState<ToolsetForMCP | null>(null);

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
            <McpToolsetCard
              key={toolset.id}
              toolset={toolset}
              onConfigClick={() => {
                setConfigModalToolset(toolset);
                setConfigModalOpen(true);
              }}
            />
          ))}
        </div>
        <Dialog open={configModalOpen} onOpenChange={setConfigModalOpen}>
          <Dialog.Content className="max-w-3xl! p-10!">
            <Dialog.Header>
              <Dialog.Title>MCP Config</Dialog.Title>
            </Dialog.Header>
            {configModalToolset && (
              <MCPJson
                toolset={configModalToolset}
                fullWidth
                className="max-h-[70vh] overflow-y-auto"
              />
            )}
            <Dialog.Footer>
              <Button onClick={() => setConfigModalOpen(false)}>Close</Button>
            </Dialog.Footer>
          </Dialog.Content>
        </Dialog>
      </Page.Section.Body>
    </Page.Section>
  );
}

export function McpToolsetCard({
  toolset,
  onConfigClick,
}: {
  toolset: ToolsetForMCP;
  onConfigClick: () => void;
}) {
  const routes = useRoutes();

  return (
    <ServerCard
      toolset={toolset}
      onCardClick={() => routes.mcp.details.goTo(toolset.slug)}
      additionalActions={[
        {
          label: "View/Copy Config",
          onClick: onConfigClick,
          icon: "braces",
        },
      ]}
    />
  );
}
