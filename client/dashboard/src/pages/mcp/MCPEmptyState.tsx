import { Text } from "@/components/ui/Text";
import { Network } from "lucide-react";

export function MCPEmptyState({ cta }: { cta?: React.ReactNode }): JSX.Element {
  return (
    // No heading or description: the page above the tabs already carries
    // both, and repeating them made the empty state read as a second page.
    // Without those slots a Page.Section buys nothing, so this is the body.
    <div className="mb-6">
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
    </div>
  );
}
