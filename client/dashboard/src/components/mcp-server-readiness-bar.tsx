import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { ChevronDown } from "lucide-react";
import * as React from "react";
import { Link } from "react-router";

export type ReadinessCheck = {
  key: string;
  label: string;
  description: string;
  ready: boolean;
  href?: string;
};

// Collapsed-by-default readiness summary for the MCP server sidebar — a row
// of brand-colored segments (one per check) that expands into per-check rows
// linking to the sub-page where that check can be resolved. Mirrors the
// "Essentials" checklist pattern from the production dashboard, restyled to
// match Gram's existing sidebar card language (McpSidebarNavShell's card)
// instead of copying its pill/badge treatment verbatim.
export function McpServerReadinessBar({
  checks,
}: {
  checks: ReadinessCheck[];
}): React.JSX.Element | null {
  const [expanded, setExpanded] = React.useState(false);

  if (checks.length === 0) return null;

  const readyCount = checks.filter((c) => c.ready).length;
  const allReady = readyCount === checks.length;
  const remaining = checks.length - readyCount;

  return (
    <div className="bg-card border-border dark:bg-neutral-950 flex flex-col rounded-lg border shadow-md">
      <button
        type="button"
        onClick={() => setExpanded((prev) => !prev)}
        className="flex w-full flex-col gap-2 px-4 py-3 text-left"
        aria-expanded={expanded}
      >
        <div className="flex items-center justify-between gap-2">
          <div className="flex flex-1 gap-1">
            {checks.map((check) => (
              <span
                key={check.key}
                className={cn(
                  "h-1.5 flex-1 rounded-full",
                  check.ready
                    ? "bg-[var(--color-feedback-green-500)]"
                    : "bg-warning-foreground",
                )}
              />
            ))}
          </div>
          <ChevronDown
            className={cn(
              "text-muted-foreground h-3.5 w-3.5 shrink-0 transition-transform",
              expanded && "rotate-180",
            )}
          />
        </div>
        <Type variant="small" muted className="text-xs">
          {allReady
            ? "This MCP server is ready to be used."
            : `This MCP server needs ${remaining} more step${remaining > 1 ? "s" : ""} before it's ready.`}
        </Type>
      </button>

      {expanded && (
        <div className="border-border flex flex-col border-t">
          {checks.map((check, idx) => {
            const row = (
              <div className="flex items-start gap-2.5 px-4 py-2.5">
                <span
                  className={cn(
                    "mt-1 h-2 w-2 shrink-0 rounded-full",
                    check.ready
                      ? "bg-[var(--color-feedback-green-500)]"
                      : "bg-warning-foreground",
                  )}
                />
                <div className="flex min-w-0 flex-1 flex-col">
                  <Type variant="small" className="font-medium">
                    {check.label}
                  </Type>
                  <Type variant="small" muted className="text-xs">
                    {check.description}
                  </Type>
                </div>
              </div>
            );
            return (
              <div
                key={check.key}
                className={cn(idx > 0 && "border-border border-t", "group")}
              >
                {check.href ? (
                  <Link
                    to={check.href}
                    className="hover:bg-muted/50 block transition-colors hover:no-underline"
                  >
                    {row}
                  </Link>
                ) : (
                  row
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
