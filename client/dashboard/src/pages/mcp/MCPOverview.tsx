import { MCPCard } from "@/components/mcp/MCPCard";
import { Page } from "@/components/page-layout";
import { Dialog } from "@/components/ui/dialog";
import { ToolsetEntry } from "@gram/client/models/components";
import { Button } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { Outlet } from "react-router";
import { useToolsets } from "../toolsets/Toolsets";
import { MCPJson } from "./MCPDetails";
import { MCPEmptyState } from "./MCPEmptyState";

export function MCPRoot() {
  return <Outlet />;
}

export function MCPOverview() {
  const toolsets = useToolsets();
  const [configModalOpen, setConfigModalOpen] = useState(false);
  const [configModalToolset, setConfigModalToolset] =
    useState<ToolsetEntry | null>(null);

  if (!toolsets.isLoading && toolsets.length === 0) {
    return <MCPEmptyState />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>Hosted MCP Servers</Page.Section.Title>
          <Page.Section.Description>
            Each source is exposed as an MCP server. First-party sources like functions and OpenAPI specs are private by default, while catalog servers are public.
          </Page.Section.Description>
          <Page.Section.Body>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {toolsets.map((toolset) => (
            <MCPCard
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
      </Page.Body>
    </Page>
  );
}
