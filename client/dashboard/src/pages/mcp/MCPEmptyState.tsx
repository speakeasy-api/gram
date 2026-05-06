import { EmptyStateCard } from "@/components/empty-state-card";
import { Page } from "@/components/page-layout";
import { Network } from "lucide-react";

export function MCPEmptyState({ cta }: { cta?: React.ReactNode }) {
  return (
    <Page.Section>
      <Page.Section.Title>MCP Servers</Page.Section.Title>
      <Page.Section.Description className="max-w-2xl">
        Hosted MCP servers expose your tools to Claude Desktop, Cursor, or any
        MCP client.
      </Page.Section.Description>
      <Page.Section.Body>
        <EmptyStateCard
          icon={<Network />}
          heading="No MCP servers yet"
          description="Create an MCP server to expose tools generated from your sources."
          cta={cta}
        />
      </Page.Section.Body>
    </Page.Section>
  );
}
