import { useState, type JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { SearchIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { gramAdminFetch } from "@/lib/gramAdminApi";

export type AdminMcpTool = {
  name: string;
  title: string;
  description: string;
  access: "read" | "approval" | "write";
};

const accessLabels = {
  read: "Read-only",
  approval: "Approval flow",
  write: "Direct write",
} as const;

export function AdminMcpTools(): JSX.Element {
  const [search, setSearch] = useState("");
  const [access, setAccess] = useState<"all" | AdminMcpTool["access"]>("all");
  const toolsQuery = useQuery({
    queryKey: ["admin-mcp", "tools"],
    queryFn: ({ signal }) =>
      gramAdminFetch<{ tools: AdminMcpTool[] }>("/admin-mcp/tools", {
        signal,
        cache: "no-store",
      }),
    retry: false,
  });
  const tools = toolsQuery.data?.tools ?? [];
  const query = search.trim().toLowerCase();
  const visibleTools = tools.filter(
    (tool) =>
      (access === "all" || tool.access === access) &&
      `${tool.name} ${tool.title} ${tool.description}`
        .toLowerCase()
        .includes(query),
  );

  let content: JSX.Element;
  if (toolsQuery.isPending) {
    content = (
      <p role="status" className="text-muted-foreground py-6 text-sm">
        Loading available tools…
      </p>
    );
  } else if (toolsQuery.isError) {
    content = (
      <div role="alert" className="space-y-3 rounded-lg border p-4">
        <p className="text-sm">
          The tool list is unavailable. Admin MCP may be disabled on this
          server, or the connection may have failed.
        </p>
        <Button
          variant="outline"
          size="sm"
          disabled={toolsQuery.isFetching}
          onClick={() => void toolsQuery.refetch()}
        >
          Try again
        </Button>
      </div>
    );
  } else if (tools.length === 0) {
    content = (
      <p className="text-muted-foreground rounded-lg border p-4 text-sm">
        No tools are enabled on this server.
      </p>
    );
  } else {
    content = (
      <>
        <div className="relative">
          <SearchIcon
            aria-hidden="true"
            className="text-muted-foreground pointer-events-none absolute top-2.5 left-3 size-4"
          />
          <Input
            aria-label="Search tools"
            type="search"
            placeholder="Search tools by name or description…"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            className="pl-9"
          />
        </div>
        <div
          role="group"
          aria-label="Tool access"
          className="flex flex-wrap gap-2"
        >
          {(["all", "read", "approval", "write"] as const).map((value) => (
            <Button
              key={value}
              variant={access === value ? "secondary" : "outline"}
              size="sm"
              aria-pressed={access === value}
              onClick={() => setAccess(value)}
            >
              {value === "all" ? "All tools" : accessLabels[value]}
            </Button>
          ))}
        </div>
        <p role="status" className="text-muted-foreground text-xs">
          Showing {visibleTools.length} of {tools.length} tools
        </p>
        {visibleTools.length === 0 ? (
          <div className="space-y-3 rounded-lg border p-6 text-center">
            <p className="text-muted-foreground text-sm">
              No tools match your search and filter.
            </p>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setSearch("");
                setAccess("all");
              }}
            >
              Clear filters
            </Button>
          </div>
        ) : (
          <ul
            aria-label="Available Admin MCP tools"
            tabIndex={0}
            className="max-h-[28rem] divide-y overflow-y-auto rounded-lg border focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          >
            {visibleTools.map((tool) => (
              <li key={tool.name} className="space-y-2 p-4">
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <h3 className="min-w-0 text-sm font-medium break-words">
                    {tool.title || tool.name}
                  </h3>
                  <Badge
                    variant={tool.access === "read" ? "secondary" : "outline"}
                  >
                    {accessLabels[tool.access]}
                  </Badge>
                </div>
                <code className="text-muted-foreground block text-xs break-all">
                  {tool.name}
                </code>
                <p className="text-muted-foreground text-sm break-words">
                  {tool.description}
                </p>
              </li>
            ))}
          </ul>
        )}
        <p className="text-muted-foreground text-xs leading-relaxed">
          Approval flow tools prepare, then execute changes after your browser
          approval. Direct write tools can act without a separate approval; read
          their descriptions before use. Both require write access.
        </p>
      </>
    );
  }

  return (
    <section aria-labelledby="admin-mcp-tools-heading" className="space-y-4">
      <div className="space-y-2">
        <h2 id="admin-mcp-tools-heading" className="text-lg font-semibold">
          Available tools
        </h2>
        <p className="text-muted-foreground text-sm">
          Explore the tools enabled on this server before connecting. Your staff
          permissions are checked on every call.
        </p>
      </div>

      {content}
    </section>
  );
}
