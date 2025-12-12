import { Page } from "@/components/page-layout";
import { ServerCard } from "@/components/server-card";
import { Button } from "@speakeasy-api/moonshine";
import { Dialog } from "@/components/ui/dialog";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { useEffect, useState } from "react";
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

  // Workaround for Radix UI bug where pointer-events: none gets stuck on body
  // after closing a dialog opened from a dropdown menu
  // See: https://github.com/radix-ui/primitives/issues/1241
  // See: https://github.com/radix-ui/primitives/issues/3317
  useEffect(() => {
    if (!mcpModalOpen) {
      // Small delay to let Radix finish its cleanup, then force remove pointer-events
      const timeoutId = setTimeout(() => {
        document.body.style.pointerEvents = "";
      }, 100);
      return () => clearTimeout(timeoutId);
    }
  }, [mcpModalOpen]);

  const handleOpenMcpModal = () => {
    // Delay to ensure dropdown closes before dialog opens
    // Prevents race condition between dropdown and dialog overlays
    setTimeout(() => {
      setMcpModalOpen(true);
    }, 150);
  };

  const handleCloseModal = (e?: React.MouseEvent) => {
    // Stop all event propagation
    if (e) {
      e.preventDefault();
      e.stopPropagation();
    }
    // Close the modal and force clear body pointer-events
    setMcpModalOpen(false);
    // Force clear pointer-events immediately as well
    setTimeout(() => {
      document.body.style.pointerEvents = "";
    }, 0);
  };

  return (
    <>
      <ServerCard
        toolset={toolset}
        onCardClick={() => routes.mcp.details.goTo(toolset.slug)}
        additionalActions={[
          {
            label: "View/Copy Config",
            onClick: handleOpenMcpModal,
            icon: "braces",
          },
        ]}
      />
      <Dialog open={mcpModalOpen} onOpenChange={setMcpModalOpen}>
        <Dialog.Content
          className="!max-w-3xl !p-10"
          onInteractOutside={(e) => {
            // Prevent closing via clicking outside
            e.preventDefault();
          }}
        >
          <Dialog.Header>
            <Dialog.Title>MCP Config</Dialog.Title>
          </Dialog.Header>
          <MCPJson toolset={toolset} fullWidth />
          <div className="flex justify-end mt-4">
            <Button onClick={handleCloseModal}>Close</Button>
          </div>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
