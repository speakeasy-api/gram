import { Page } from "@/components/page-layout";
import { Type } from "@/components/ui/type";
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
        <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
          <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
            <Network className="text-muted-foreground h-6 w-6" />
          </div>
          <Type variant="subheading" className="mb-1">
            No MCP servers yet
          </Type>
          <Type small muted className="mb-4 max-w-md text-center">
            Create an MCP server to expose tools generated from your sources.
          </Type>
          {cta}
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}
