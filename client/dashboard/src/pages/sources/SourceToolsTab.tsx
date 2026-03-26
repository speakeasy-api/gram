import { SearchBar } from "@/components/ui/search-bar";
import { Type } from "@/components/ui/type";
import type { Tool } from "@/lib/toolTypes";
import { Badge } from "@speakeasy-api/moonshine";
import { useState } from "react";

type HttpTool = Extract<Tool, { type: "http" }>;
type FunctionTool = Extract<Tool, { type: "function" }>;

const HTTP_METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"] as const;

const HTTP_METHOD_VARIANT: Record<
  string,
  "success" | "information" | "warning" | "neutral" | "destructive"
> = {
  GET: "success",
  POST: "information",
  PUT: "warning",
  PATCH: "neutral",
  DELETE: "destructive",
};

function runtimeBadgeVariant(
  runtime: string,
): "success" | "information" | "warning" | "neutral" | "destructive" {
  const rt = runtime.toLowerCase();
  if (rt.startsWith("nodejs") || rt.startsWith("node")) return "information";
  if (rt.startsWith("python")) return "success";
  if (rt.startsWith("go") || rt.startsWith("golang")) return "warning";
  if (rt.startsWith("rust")) return "destructive";
  return "neutral";
}

function FilterPill({
  label,
  count,
  active,
  variant,
  onClick,
}: {
  label: string;
  count: number;
  active: boolean;
  variant: "success" | "information" | "warning" | "neutral" | "destructive";
  onClick: () => void;
}) {
  return (
    <button onClick={onClick}>
      <Badge
        variant={variant}
        className={`py-2 ${active ? "" : "opacity-50 hover:opacity-100"}`}
      >
        <Badge.Text>
          {label} ({count})
        </Badge.Text>
      </Badge>
    </button>
  );
}

function HttpToolRow({ tool }: { tool: HttpTool }) {
  return (
    <div className="grid grid-cols-[80px_40%_1fr] items-center px-4 py-3 border-b last:border-b-0 hover:bg-muted/30 transition-colors">
      <div>
        <Badge variant={HTTP_METHOD_VARIANT[tool.httpMethod] ?? "neutral"}>
          <Badge.Text>{tool.httpMethod}</Badge.Text>
        </Badge>
      </div>
      <div className="font-mono text-sm text-muted-foreground truncate pr-3">
        {tool.path}
      </div>
      <div className="text-sm truncate">{tool.name}</div>
    </div>
  );
}

function FunctionToolRow({ tool }: { tool: FunctionTool }) {
  return (
    <div className="grid grid-cols-[120px_1fr_1.5fr] gap-4 items-center px-4 py-3 border-b last:border-b-0 hover:bg-muted/30 transition-colors">
      <div>
        <Badge variant={runtimeBadgeVariant(tool.runtime)}>
          <Badge.Text>{tool.runtime}</Badge.Text>
        </Badge>
      </div>
      <div className="font-mono text-sm truncate">{tool.name}</div>
      <div className="text-sm text-muted-foreground truncate">
        {tool.description}
      </div>
    </div>
  );
}

function ToolsTableHeader({
  isOpenAPI,
  searchQuery,
  onSearchChange,
}: {
  isOpenAPI: boolean;
  searchQuery: string;
  onSearchChange: (v: string) => void;
}) {
  const columnClass =
    "text-xs font-medium text-muted-foreground uppercase tracking-wider";
  return (
    <div className="border-b bg-muted/50 shrink-0">
      {isOpenAPI ? (
        <div className="grid grid-cols-[80px_40%_1fr] items-center px-4 py-1">
          <div className={columnClass}>Method</div>
          <div className={`${columnClass} pr-3`}>Endpoint</div>
          <div className={`${columnClass} flex items-center justify-between`}>
            <span>Tool Name</span>
            <SearchBar
              value={searchQuery}
              onChange={onSearchChange}
              placeholder="Search tools..."
              className="w-48"
            />
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-[120px_1fr_1.5fr] gap-4 items-center px-4 py-1">
          <div className={columnClass}>Runtime</div>
          <div className={columnClass}>Function Name</div>
          <div className={`${columnClass} flex items-center justify-between`}>
            <span>Description</span>
            <SearchBar
              value={searchQuery}
              onChange={onSearchChange}
              placeholder="Search tools..."
              className="w-48"
            />
          </div>
        </div>
      )}
    </div>
  );
}

function FilteredToolsList({
  tools,
  isOpenAPI,
  methodFilter,
  runtimeFilter,
  searchQuery,
}: {
  tools: Tool[];
  isOpenAPI: boolean;
  methodFilter: string | null;
  runtimeFilter: string | null;
  searchQuery: string;
}) {
  const filtered = tools.filter((tool) => {
    if (isOpenAPI) {
      if (tool.type !== "http") return false;
      if (methodFilter && tool.httpMethod !== methodFilter) return false;
      if (searchQuery) {
        const q = searchQuery.toLowerCase();
        return (
          tool.name.toLowerCase().includes(q) ||
          tool.path.toLowerCase().includes(q)
        );
      }
    } else {
      if (tool.type !== "function") return false;
      if (runtimeFilter && tool.runtime !== runtimeFilter) return false;
      if (searchQuery) {
        const q = searchQuery.toLowerCase();
        return (
          tool.name.toLowerCase().includes(q) ||
          tool.description.toLowerCase().includes(q)
        );
      }
    }
    return true;
  });

  if (filtered.length === 0) {
    return (
      <div className="flex items-center justify-center h-full">
        <Type muted>No matching tools found</Type>
      </div>
    );
  }

  return (
    <>
      {filtered.map((tool) => {
        if (tool.type === "http")
          return <HttpToolRow key={tool.toolUrn} tool={tool} />;
        if (tool.type === "function")
          return <FunctionToolRow key={tool.toolUrn} tool={tool} />;
        return null;
      })}
    </>
  );
}

export function SourceToolsTab({
  relatedTools,
  isOpenAPI,
  uniqueRuntimes,
}: {
  relatedTools: Tool[];
  isOpenAPI: boolean;
  uniqueRuntimes: string[];
}) {
  const [methodFilter, setMethodFilter] = useState<string | null>(null);
  const [runtimeFilter, setRuntimeFilter] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState("");

  if (relatedTools.length === 0) {
    return (
      <div className="max-w-[1270px] mx-auto px-8 py-6 w-full">
        <div className="text-center py-12">
          <Type muted>No tools derived from this source yet.</Type>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-[1270px] mx-auto px-8 py-6 w-full flex-1 flex flex-col min-h-0">
      <div className="flex flex-col gap-4 flex-1 min-h-0">
        {/* HTTP method filter pills */}
        {isOpenAPI && (
          <div className="flex gap-2 flex-wrap shrink-0">
            <button onClick={() => setMethodFilter(null)}>
              <Badge
                variant={methodFilter === null ? "information" : "neutral"}
                className="py-2"
              >
                <Badge.Text>
                  All ({relatedTools.filter((t) => t.type === "http").length})
                </Badge.Text>
              </Badge>
            </button>
            {HTTP_METHODS.map((method) => {
              const count = relatedTools.filter(
                (t) => t.type === "http" && t.httpMethod === method,
              ).length;
              if (count === 0) return null;
              return (
                <FilterPill
                  key={method}
                  label={method}
                  count={count}
                  active={methodFilter === method}
                  variant={HTTP_METHOD_VARIANT[method]}
                  onClick={() =>
                    setMethodFilter(methodFilter === method ? null : method)
                  }
                />
              );
            })}
          </div>
        )}

        {/* Runtime filter pills (only when multiple runtimes) */}
        {!isOpenAPI && uniqueRuntimes.length > 1 && (
          <div className="flex gap-2 flex-wrap shrink-0">
            <button onClick={() => setRuntimeFilter(null)}>
              <Badge
                variant={runtimeFilter === null ? "information" : "neutral"}
                className="py-2"
              >
                <Badge.Text>
                  All (
                  {relatedTools.filter((t) => t.type === "function").length})
                </Badge.Text>
              </Badge>
            </button>
            {uniqueRuntimes.map((runtime) => {
              const count = relatedTools.filter(
                (t) => t.type === "function" && t.runtime === runtime,
              ).length;
              return (
                <FilterPill
                  key={runtime}
                  label={runtime}
                  count={count}
                  active={runtimeFilter === runtime}
                  variant={runtimeBadgeVariant(runtime)}
                  onClick={() =>
                    setRuntimeFilter(runtimeFilter === runtime ? null : runtime)
                  }
                />
              );
            })}
          </div>
        )}

        {/* Tools table */}
        <div className="border rounded-lg flex flex-col overflow-hidden flex-1 min-h-0 mb-4">
          <ToolsTableHeader
            isOpenAPI={isOpenAPI}
            searchQuery={searchQuery}
            onSearchChange={setSearchQuery}
          />
          <div className="flex-1 overflow-y-auto">
            <FilteredToolsList
              tools={relatedTools}
              isOpenAPI={isOpenAPI}
              methodFilter={methodFilter}
              runtimeFilter={runtimeFilter}
              searchQuery={searchQuery}
            />
          </div>
        </div>
      </div>
    </div>
  );
}
