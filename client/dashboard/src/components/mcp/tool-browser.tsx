import { Badge } from "@/components/ui/Badge";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { ReactNode } from "react";
import type { ToolParameter } from "./tool-parameters";

/**
 * The tool-browsing primitives shared by every MCP server surface that lists
 * tools it does not own: a row per tool, and the parameter list its details
 * drawer shows. Remote MCP servers and built-in servers both advertise nothing
 * but a name, a description and a JSON Schema, so neither can reuse the
 * toolset Tools tab (ToolList), which is built around Speakeasy-side tool
 * identity, variations and edit actions.
 *
 * Deliberately presentational: selection, drawers and empty states belong to
 * the surface, which knows what its own failures mean.
 */

/** The bordered strip the rows sit in, matching the toolset Tools tab. */
export function McpToolRows({
  children,
}: {
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="border-neutral-softest w-full overflow-hidden border">
      {children}
    </div>
  );
}

/**
 * A single tool row: name on top, description truncated to one line below, and
 * any surface-specific hints beside the name. Selecting it is the only action —
 * these tools have no Speakeasy-side identity to edit.
 */
export function McpToolRow({
  name,
  description,
  hint,
  selected,
  onSelect,
}: {
  name: string;
  description?: string;
  /** Badges or indicators rendered beside the name. */
  hint?: ReactNode;
  selected: boolean;
  onSelect: () => void;
}): JSX.Element {
  return (
    <div
      role="button"
      tabIndex={0}
      aria-haspopup="dialog"
      aria-expanded={selected}
      onClick={onSelect}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSelect();
        }
      }}
      className={cn(
        "border-neutral-softest hover:bg-muted flex cursor-pointer items-center justify-between border-b py-4 pr-3 pl-4 transition-colors last:border-b-0",
        selected && "bg-muted",
      )}
    >
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex min-w-0 items-center gap-2">
          <p className="text-foreground truncate text-sm leading-6">{name}</p>
          {hint}
        </div>
        <p className="text-muted-foreground truncate text-sm leading-6">
          {description || "No description"}
        </p>
      </div>
    </div>
  );
}

export function McpToolRowsSkeleton({
  rows = 5,
}: {
  rows?: number;
}): JSX.Element {
  return (
    <McpToolRows>
      {Array.from({ length: rows }).map((_, index) => (
        <div
          key={index}
          className="border-neutral-softest flex flex-col gap-2 border-b py-4 pr-3 pl-4 last:border-b-0"
        >
          <Skeleton className="h-4 w-48" />
          <Skeleton className="h-3 w-80 max-w-full" />
        </div>
      ))}
    </McpToolRows>
  );
}

/**
 * The tool's inputs, read from its JSON Schema. A schema dump is not something
 * anyone reads to decide whether a tool does what they need, so the drawer
 * shows the arguments as a list instead.
 */
export function McpToolParameters({
  parameters,
}: {
  parameters: ToolParameter[];
}): JSX.Element {
  if (parameters.length === 0) {
    return (
      <Text muted small as="p">
        This tool takes no parameters.
      </Text>
    );
  }
  return (
    <ul className="divide-border divide-y">
      {parameters.map((parameter) => (
        <li
          key={parameter.name}
          className="flex flex-col gap-1 py-3 first:pt-0 last:pb-0"
        >
          <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
            <Text mono small as="span" className="font-medium break-all">
              {parameter.name}
            </Text>
            <Text mono small as="span" className="text-muted-foreground">
              {parameter.type}
            </Text>
            {parameter.required ? (
              <Badge variant="neutral" size="sm" background>
                <Badge.Text className="text-[10px] uppercase">
                  Required
                </Badge.Text>
              </Badge>
            ) : null}
          </div>
          {parameter.description ? (
            <Text muted small as="span" className="break-words">
              {parameter.description}
            </Text>
          ) : null}
        </li>
      ))}
    </ul>
  );
}
