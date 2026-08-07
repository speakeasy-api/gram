import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { cn } from "@/lib/utils";
import { ChevronRight, Folder, Tag } from "lucide-react";
import type { ScopeSelection, ScopeTreeNode } from "./businessMemoryScopes";

function selectionIsActive(
  selection: ScopeSelection | null,
  candidate: ScopeSelection,
): boolean {
  return (
    selection?.kind === candidate.kind && selection.value === candidate.value
  );
}

export function BusinessMemoryScopeTree({
  nodes,
  selection,
  onSelectionChange,
  totalMemories,
  loading,
  error,
}: {
  nodes: ScopeTreeNode[];
  selection: ScopeSelection | null;
  onSelectionChange: (selection: ScopeSelection | null) => void;
  totalMemories: number;
  loading: boolean;
  error: boolean;
}): JSX.Element {
  const toggleSelection = (candidate: ScopeSelection) => {
    onSelectionChange(
      selectionIsActive(selection, candidate) ? null : candidate,
    );
  };

  return (
    <aside className="border-border bg-card min-w-0 border p-2">
      <div className="text-muted-foreground px-2 py-1.5 text-xs font-medium">
        Content scope
      </div>
      <button
        type="button"
        onClick={() => onSelectionChange(null)}
        className={cn(
          "hover:bg-muted flex w-full items-center gap-2 px-2 py-1.5 text-left text-sm transition-colors",
          selection === null && "bg-muted font-medium",
        )}
      >
        <Folder className="text-muted-foreground size-4 shrink-0" />
        <span className="min-w-0 flex-1 truncate">All memories</span>
        <span className="text-muted-foreground tabular-nums">
          {totalMemories}
        </span>
      </button>

      {loading ? (
        <p className="text-muted-foreground px-2 py-3 text-xs">
          Loading scopes…
        </p>
      ) : error ? (
        <p className="text-destructive px-2 py-3 text-xs">
          Unable to load scopes
        </p>
      ) : nodes.length === 0 ? (
        <p className="text-muted-foreground px-2 py-3 text-xs">
          No content scopes
        </p>
      ) : (
        <div className="mt-1">
          {nodes.map((node) => {
            const namespaceSelection: ScopeSelection = {
              kind: "namespace",
              value: node.namespace,
            };
            return (
              <Collapsible key={node.namespace}>
                <CollapsibleTrigger
                  onClick={() => toggleSelection(namespaceSelection)}
                  className={cn(
                    "hover:bg-muted flex w-full items-center gap-1 px-1 py-1.5 text-left text-sm transition-colors [&[data-state=open]_.scope-chevron]:rotate-90",
                    selectionIsActive(selection, namespaceSelection) &&
                      "bg-muted font-medium",
                  )}
                >
                  <ChevronRight className="scope-chevron text-muted-foreground size-3.5 shrink-0 transition-transform" />
                  <Folder className="text-muted-foreground size-4 shrink-0" />
                  <span className="min-w-0 flex-1 truncate">
                    {node.namespace}
                  </span>
                  <span className="text-muted-foreground tabular-nums">
                    {node.memoryCount}
                  </span>
                </CollapsibleTrigger>
                <CollapsibleContent className="ml-4 border-l pl-1">
                  {node.children.map((child) => {
                    const tagSelection: ScopeSelection = {
                      kind: "tag",
                      value: child.tag,
                    };
                    return (
                      <button
                        key={child.tag}
                        type="button"
                        onClick={() => toggleSelection(tagSelection)}
                        className={cn(
                          "hover:bg-muted flex w-full items-center gap-2 px-2 py-1.5 text-left text-sm transition-colors",
                          selectionIsActive(selection, tagSelection) &&
                            "bg-muted font-medium",
                        )}
                      >
                        <Tag className="text-muted-foreground size-3.5 shrink-0" />
                        <span className="min-w-0 flex-1 truncate">
                          {child.label}
                        </span>
                        <span className="text-muted-foreground tabular-nums">
                          {child.memoryCount}
                        </span>
                      </button>
                    );
                  })}
                </CollapsibleContent>
              </Collapsible>
            );
          })}
        </div>
      )}
    </aside>
  );
}
