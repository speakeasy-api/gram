import { InputDialog } from "@/components/input-dialog";
import { MCPCard } from "@/components/mcp/MCPCard";
import { Page } from "@/components/page-layout";
import { useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { Button } from "@speakeasy-api/moonshine";
import { Plus } from "lucide-react";
import { useState } from "react";
import { Outlet, useNavigate } from "react-router";
import { toast } from "sonner";
import { useToolsets } from "../toolsets/Toolsets";
import { MCPEmptyState } from "./MCPEmptyState";

export function MCPRoot() {
  return <Outlet />;
}

export function MCPOverview() {
  const toolsets = useToolsets();
  const routes = useRoutes();
  const navigate = useNavigate();
  const client = useSdkClient();

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
          <Page.Section.CTA>
            <Button onClick={() => setNewMcpDialogOpen(true)}>
              <Button.LeftIcon>
                <Plus />
              </Button.LeftIcon>
              <Button.Text>New MCP Server</Button.Text>
            </Button>
          </Page.Section.CTA>
          <Page.Section.Description>
            Each source is exposed as an MCP server. First-party sources like
            functions and OpenAPI specs are private by default, while catalog
            servers are public.
          </Page.Section.Description>
          <Page.Section.Body>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
              {toolsets.map((toolset) => (
                <MCPCard key={toolset.id} toolset={toolset} />
              ))}
            </div>
          </Page.Section.Body>
        </Page.Section>
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
      </Page.Body>
    </Page>
  );
}
