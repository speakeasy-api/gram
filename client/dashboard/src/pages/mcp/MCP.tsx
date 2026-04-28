import { InputDialog } from "@/components/input-dialog";
import { RequireScope } from "@/components/require-scope";
import { BuiltInMCPCard } from "@/components/mcp/BuiltInMCPCard";
import { MCPCard, MCPCardSkeleton } from "@/components/mcp/MCPCard";
import { MCPTableRow, MCPTableRowSkeleton } from "@/components/mcp/MCPTableRow";
import { Page } from "@/components/page-layout";
import { DotTable } from "@/components/ui/dot-table";
import { ViewToggle } from "@/components/ui/view-toggle";
import { useViewMode } from "@/components/ui/use-view-mode";
import { useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { Button } from "@speakeasy-api/moonshine";
import { Plus } from "lucide-react";
import { useState } from "react";
import { Outlet, useNavigate } from "react-router";
import { toast } from "sonner";
import { useToolsets } from "../toolsets/useToolsets";
import { MCPEmptyState } from "./MCPEmptyState";

const BUILT_IN_SERVERS = [
  {
    name: "MCP Logs",
    description:
      "Search and analyze your project's MCP server logs, tool calls, and agent sessions.",
    slug: "logs",
  },
];

export function MCPRoot() {
  return <Outlet />;
}

export const MCPPage = () => {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["mcp:read", "mcp:write"]} level="page">
          <MCPOverview />
        </RequireScope>
      </Page.Body>
    </Page>
  );
};

export function MCPOverview() {
  const toolsets = useToolsets();
  const routes = useRoutes();
  const navigate = useNavigate();
  const client = useSdkClient();

  const isLoading = toolsets.isLoading;

  const [viewMode, setViewMode] = useViewMode();
  const [newMcpDialogOpen, setNewMcpDialogOpen] = useState(false);
  const [newMcpServerName, setNewMcpServerName] = useState("");

  const handleCreateMcpServerSubmit = async () => {
    const result = await client.toolsets.create({
      createToolsetRequestBody: {
        name: newMcpServerName,
      },
    });

    toast.success(`MCP server "${result.name}" created`);

    navigate(routes.mcp.details.href(result.slug) + "#tools");
  };

  const newMcpServerButton = (
    <RequireScope scope="mcp:write" level="component">
      <Button size="sm" onClick={() => setNewMcpDialogOpen(true)}>
        <Button.LeftIcon>
          <Plus />
        </Button.LeftIcon>
        <Button.Text>New MCP Server</Button.Text>
      </Button>
    </RequireScope>
  );

  const newMcpServerDialog = (
    <InputDialog
      open={newMcpDialogOpen}
      onOpenChange={setNewMcpDialogOpen}
      title="Create MCP Server"
      description={`Create a new MCP server`}
      submitButtonText="Create"
      inputs={{
        label: "MCP server name",
        placeholder: "My MCP Server",
        value: newMcpServerName,
        onChange: setNewMcpServerName,
        onSubmit: handleCreateMcpServerSubmit,
        validate: (value) => value.length > 0 && value.length <= 40,
        hint: (value) => (
          <div className="flex w-full justify-between">
            <p className="text-destructive">
              {value.length > 40 && "Must be 40 characters or less"}
            </p>
            <p>{value.length}/40</p>
          </div>
        ),
      }}
    />
  );

  const builtInSection = (
    <Page.Section>
      <Page.Section.Title>Built-in MCP Servers</Page.Section.Title>
      <Page.Section.Description>
        Pre-configured MCP servers provided by Gram for your project. Connect
        from Claude Desktop, Cursor, or any MCP client.
      </Page.Section.Description>
      <Page.Section.Body>
        <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
          {BUILT_IN_SERVERS.map((server) => (
            <BuiltInMCPCard key={server.slug} {...server} />
          ))}
        </div>
      </Page.Section.Body>
    </Page.Section>
  );

  if (!isLoading && toolsets.length === 0) {
    return (
      <>
        <MCPEmptyState cta={newMcpServerButton} />
        {builtInSection}
        {newMcpServerDialog}
      </>
    );
  }

  return (
    <>
      <Page.Section>
        <Page.Section.Title>Hosted MCP Servers</Page.Section.Title>
        <Page.Section.CTA>
          <ViewToggle value={viewMode} onChange={setViewMode} />
        </Page.Section.CTA>
        <Page.Section.CTA>{newMcpServerButton}</Page.Section.CTA>
        <Page.Section.Description className="max-w-2xl">
          Each source is exposed as an MCP server. First-party sources like
          functions and OpenAPI specs are private by default, while catalog
          servers are public.
        </Page.Section.Description>
        <Page.Section.Body>
          {viewMode === "grid" ? (
            <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
              {isLoading ? (
                <>
                  <MCPCardSkeleton />
                  <MCPCardSkeleton />
                </>
              ) : (
                toolsets.map((toolset) => (
                  <MCPCard key={toolset.id} toolset={toolset} />
                ))
              )}
            </div>
          ) : (
            <DotTable
              headers={[
                { label: "Name" },
                { label: "Visibility" },
                { label: "URL" },
                { label: "Tools" },
              ]}
            >
              {isLoading ? (
                <>
                  <MCPTableRowSkeleton />
                  <MCPTableRowSkeleton />
                </>
              ) : (
                toolsets.map((toolset) => (
                  <MCPTableRow key={toolset.id} toolset={toolset} />
                ))
              )}
            </DotTable>
          )}
        </Page.Section.Body>
      </Page.Section>
      {builtInSection}
      {newMcpServerDialog}
    </>
  );
}
