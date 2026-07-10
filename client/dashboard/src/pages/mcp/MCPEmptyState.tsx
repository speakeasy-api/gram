import { Page } from "@/components/page-layout";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
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
        <InlineEmptyState
          icon={<Network />}
          title="No MCP servers yet"
          description="Create an MCP server to expose tools generated from your sources."
          action={cta}
          className="py-16"
        />
      </Page.Section.Body>
    </Page.Section>
  );
}
