import { Page } from "@/components/page-layout";
import { Text } from "@/components/ui/Text";
import { Network } from "lucide-react";

export function MCPEmptyState({ cta }: { cta?: React.ReactNode }): JSX.Element {
  return (
    <Page.Section>
      <Page.Section.Title>MCP Servers</Page.Section.Title>
      <Page.Section.Description className="max-w-2xl">
        Hosted MCP servers expose your tools to Claude Desktop, Cursor, or any
        MCP client.
      </Page.Section.Description>
      <Page.Section.Body>
        <div className="bg-muted/20 flex flex-col items-center justify-center border border-dashed px-8 py-16">
          <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
            <Network className="text-muted-foreground h-6 w-6" />
          </div>
          <Text variant="subheading" className="mb-1">
            No MCP servers yet
          </Text>
          <Text small muted className="mb-4 max-w-md text-center">
            Add a server to bring it under the gateway — pick one from the
            catalog, or connect one you already run.
          </Text>
          {cta}
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}
