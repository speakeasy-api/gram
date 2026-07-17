import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import { Type } from "@/components/ui/type";
import { toolVariationsGroupDisplayName } from "@/lib/toolVariationGroups";
import { cn } from "@/lib/utils";
import { ListToolFiltersResult } from "@gram/client/models/components/listtoolfiltersresult.js";
import { ToolFilterTool } from "@gram/client/models/components/toolfiltertool.js";
import { Stack } from "@speakeasy-api/moonshine";

// Sentinel value identifying the "available only without filtering" grouping in
// the active-tag selection, distinct from any real tag.
export const EXCLUDED_TAG_KEY = "__excluded__";
const EXCLUDED_LABEL = "Available only without filtering";

/**
 * Read-only panel that surfaces the tool filter scopes (tags) configured for an
 * MCP server's resolved variation group and the tools under each, using the same
 * effective-tag derivation as the runtime `?tags=` filter. Clicking a chip
 * filters the ToolList below; the accordion reveals the tool names under each
 * scope. Only rendered when filtering is enabled (an explicit group is set).
 */
export function MCPToolFilterScopesPanel({
  filters,
  activeTag,
  onSelectTag,
}: {
  filters: ListToolFiltersResult;
  activeTag: string | null;
  onSelectTag: (tag: string | null) => void;
}): JSX.Element {
  const hasExcluded = filters.excluded.length > 0;

  return (
    <Stack
      gap={3}
      className="border-border bg-muted/20 mb-4 rounded-lg border p-4"
    >
      <Stack direction="horizontal" justify="space-between" align="center">
        <Type variant="small" className="font-medium">
          Tool filtering
        </Type>
        {filters.toolVariationsGroupName && (
          <Type variant="small" muted>
            Group:{" "}
            {toolVariationsGroupDisplayName(filters.toolVariationsGroupName)}
          </Type>
        )}
      </Stack>

      <Type variant="small" muted>
        Clients can request a subset of tools with the <code>?tags=</code> query
        parameter. Select a scope to preview the tools it exposes.
      </Type>

      {/* Filter chips: clicking one scopes the tool list below to its tools. */}
      <Stack direction="horizontal" gap={2} className="flex-wrap">
        <FilterChip
          label="All tools"
          active={activeTag === null}
          onClick={() => onSelectTag(null)}
        />
        {filters.scopes.map((scope) => (
          <FilterChip
            key={scope.tag}
            label={scope.tag}
            count={scope.toolCount}
            active={activeTag === scope.tag}
            onClick={() =>
              onSelectTag(activeTag === scope.tag ? null : scope.tag)
            }
          />
        ))}
        {hasExcluded && (
          <FilterChip
            label={EXCLUDED_LABEL}
            count={filters.excluded.length}
            active={activeTag === EXCLUDED_TAG_KEY}
            onClick={() =>
              onSelectTag(
                activeTag === EXCLUDED_TAG_KEY ? null : EXCLUDED_TAG_KEY,
              )
            }
          />
        )}
      </Stack>

      {/* Accordion: reveals the tool names under each scope without the full
          ToolRow detail shown in the list below. When a specific scope chip is
          active, the accordion collapses to just that scope's dropdown (hiding
          the others) and opens it as a focused preview; "All tools" restores
          every dropdown. Keying on the active tag remounts so the freshly
          selected scope opens via defaultValue while staying toggleable. */}
      <Accordion
        key={activeTag ?? "all"}
        type="multiple"
        defaultValue={activeTag ? [activeTag] : []}
        className="w-full"
      >
        {filters.scopes
          .filter((scope) => activeTag === null || scope.tag === activeTag)
          .map((scope) => (
            <ScopeAccordionItem
              key={scope.tag}
              value={scope.tag}
              label={scope.tag}
              count={scope.toolCount}
              tools={scope.tools}
            />
          ))}
        {hasExcluded &&
          (activeTag === null || activeTag === EXCLUDED_TAG_KEY) && (
            <ScopeAccordionItem
              value={EXCLUDED_TAG_KEY}
              label={EXCLUDED_LABEL}
              count={filters.excluded.length}
              tools={filters.excluded}
            />
          )}
      </Accordion>
    </Stack>
  );
}

function FilterChip({
  label,
  count,
  active,
  onClick,
}: {
  label: string;
  count?: number;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <Badge asChild variant={active ? "default" : "outline"}>
      <button type="button" onClick={onClick} className="cursor-pointer">
        {/* Tags render verbatim: ?tags= matching is case-sensitive, so the
            displayed label must be the exact tag string. */}
        <span>{label}</span>
        {count !== undefined && (
          <span className={cn(active ? "opacity-80" : "text-muted-foreground")}>
            {count}
          </span>
        )}
      </button>
    </Badge>
  );
}

function ScopeAccordionItem({
  value,
  label,
  count,
  tools,
}: {
  value: string;
  label: string;
  count: number;
  tools: ToolFilterTool[];
}) {
  return (
    <AccordionItem value={value}>
      <AccordionTrigger className="py-2 text-sm">
        <span className="flex items-center gap-2">
          <span>{label}</span>
          <span className="text-muted-foreground">
            {count} {count === 1 ? "tool" : "tools"}
          </span>
        </span>
      </AccordionTrigger>
      <AccordionContent>
        <Stack direction="horizontal" gap={2} className="flex-wrap pb-2">
          {tools.map((tool) => (
            <Badge key={tool.toolUrn} variant="secondary">
              {tool.name}
            </Badge>
          ))}
        </Stack>
      </AccordionContent>
    </AccordionItem>
  );
}
