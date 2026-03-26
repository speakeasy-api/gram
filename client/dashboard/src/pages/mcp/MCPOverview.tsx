import { InputDialog } from "@/components/input-dialog";
import { BuiltInMCPCard } from "@/components/mcp/BuiltInMCPCard";
import { MCPCard } from "@/components/mcp/MCPCard";
import { MCPTableRow } from "@/components/mcp/MCPTableRow";
import { Page } from "@/components/page-layout";
import { DotTable } from "@/components/ui/dot-table";
import { ViewToggle, useViewMode } from "@/components/ui/view-toggle";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useIsProjectEmpty } from "@/pages/onboarding/UploadOpenAPI";
import { InitialChoiceStep } from "@/pages/onboarding/Wizard";
import { useRoutes } from "@/routes";
import { Button } from "@speakeasy-api/moonshine";
import { Plus } from "lucide-react";
import { useState } from "react";
import { Outlet, useNavigate } from "react-router";
import { toast } from "sonner";
import { useToolsets } from "../toolsets/Toolsets";
import { MCPEmptyState } from "./MCPEmptyState";

const BUILT_IN_SERVERS = [
  {
    name: "MCP Logs",
    description:
      "Search and analyze your project's MCP server logs, tool calls, and chat sessions.",
    slug: "logs",
  },
];

export function MCPRoot() {
  return <Outlet />;
}

export function MCPOverview() {
  const toolsets = useToolsets();
  const routes = useRoutes();
  const navigate = useNavigate();
  const client = useSdkClient();
  const { isEmpty: isProjectEmpty, isLoading: isProjectLoading } =
    useIsProjectEmpty();
  const telemetry = useTelemetry();
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;

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
    <Button size="sm" onClick={() => setNewMcpDialogOpen(true)}>
      <Button.LeftIcon>
        <Plus />
      </Button.LeftIcon>
      <Button.Text>New MCP Server</Button.Text>
    </Button>
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
          <div className="flex justify-between w-full">
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
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
          {BUILT_IN_SERVERS.map((server) => (
            <BuiltInMCPCard key={server.slug} {...server} />
          ))}
        </div>
      </Page.Section.Body>
    </Page.Section>
  );

  if (!toolsets.isLoading && toolsets.length === 0) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          {isProjectEmpty && !isProjectLoading ? (
            <InitialChoiceStep
              routes={routes}
              isFunctionsEnabled={isFunctionsEnabled}
            />
          ) : (
            <MCPEmptyState nonEmptyProjectCTA={newMcpServerButton} />
          )}
          {builtInSection}
          {newMcpServerDialog}
        </Page.Body>
      </Page>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
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
              <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
                {toolsets.map((toolset) => (
                  <MCPCard key={toolset.id} toolset={toolset} />
                ))}
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
                {toolsets.map((toolset) => (
                  <MCPTableRow key={toolset.id} toolset={toolset} />
                ))}
              </DotTable>
            )}
          </Page.Section.Body>
        </Page.Section>
        {builtInSection}
        {newMcpServerDialog}
      </Page.Body>
    </Page>
  );
}
