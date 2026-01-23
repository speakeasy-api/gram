import { MCPCard } from "@/components/mcp/MCPCard";
import { Page } from "@/components/page-layout";
import { Outlet } from "react-router";
import { useToolsets } from "../toolsets/Toolsets";
import { MCPEmptyState } from "./MCPEmptyState";

export function MCPRoot() {
  return <Outlet />;
}

export function MCPOverview() {
  const toolsets = useToolsets();

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
            <MCPCard key={toolset.id} toolset={toolset} />
          ))}
        </div>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}
