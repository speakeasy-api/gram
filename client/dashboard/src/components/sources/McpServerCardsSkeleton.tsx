import { Skeleton } from "@/components/ui/skeleton";

/**
 * Loading placeholder for a grid of MCP server cards (linked servers on a
 * source's "MCP Servers" tab). Shared by the Remote MCP, Tunneled MCP, and
 * External MCP detail pages so the skeleton shape can't drift between them.
 */
export function McpServerCardsSkeleton(): JSX.Element {
  return (
    <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
      {[1, 2, 3].map((i) => (
        <div key={i} className="bg-card border p-6">
          <div className="flex items-center gap-3">
            <Skeleton className="h-10 w-10" />
            <div className="flex-1">
              <Skeleton className="mb-2 h-4 w-24" />
              <Skeleton className="h-3 w-32" />
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}
