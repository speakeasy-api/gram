import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { ChevronDown } from "lucide-react";
import * as React from "react";
import { Link } from "react-router";

export type ReadinessCheck = {
  key: string;
  label: string;
  description: string;
  /** null when it cannot be determined: neither done nor a step to take. */
  ready: boolean | null;
  /** An optional check never counts as a step and never blocks readiness. */
  optional?: boolean;
  /**
   * Something that is failing now, such as a tunnel with no agent, rather
   * than a setup step not yet taken. The header counts these as issues.
   */
  broken?: boolean;
  href?: string;
};

function stateClass(check: ReadinessCheck): string {
  if (check.ready === null || (check.optional && !check.ready)) {
    return "bg-muted-foreground/50";
  }
  return check.ready
    ? "bg-[var(--color-feedback-green-500)]"
    : "bg-warning-foreground";
}

// Only required checks that are known to be incomplete count as steps; one
// that cannot be determined keeps the server from being called ready.
function summary(checks: ReadinessCheck[]): string {
  const required = checks.filter((c) => !c.optional);
  const remaining = required.filter((c) => c.ready === false).length;
  if (remaining > 0) {
    return `This MCP server needs ${remaining} more step${remaining > 1 ? "s" : ""} before it's ready.`;
  }
  if (required.some((c) => c.ready === null)) {
    return "This MCP server's readiness can't be confirmed.";
  }
  return "This MCP server is ready to be used.";
}

// Collapsed-by-default readiness summary for the MCP server sidebar — a row
// of brand-colored segments (one per check) that expands into per-check rows
// linking to the sub-page where that check can be resolved. Mirrors the
// "Essentials" checklist pattern from the production dashboard, restyled to
// match Speakeasy's existing sidebar card language (DetailSidebarNav's card)
// instead of copying its pill/badge treatment verbatim.
export function McpServerReadinessBar({
  checks,
}: {
  checks: ReadinessCheck[];
}): React.JSX.Element | null {
  const [expanded, setExpanded] = React.useState(false);

  if (checks.length === 0) return null;

  return (
    <div className="bg-card border-border dark:bg-neutral-950 flex flex-col border shadow-md">
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
                className={cn("h-1.5 flex-1 rounded-full", stateClass(check))}
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
        <Text variant="small" muted className="text-xs">
          {summary(checks)}
        </Text>
      </button>

      {expanded && (
        <div className="border-border flex flex-col border-t">
          {checks.map((check, idx) => {
            const row = (
              <div className="flex items-start gap-2.5 px-4 py-2.5">
                <span
                  className={cn(
                    "mt-1 h-2 w-2 shrink-0 rounded-full",
                    stateClass(check),
                  )}
                />
                <div className="flex min-w-0 flex-1 flex-col">
                  <Text variant="small" className="font-medium">
                    {check.label}
                    {check.optional && (
                      <span className="text-muted-foreground font-normal">
                        {" "}
                        (optional)
                      </span>
                    )}
                  </Text>
                  <Text variant="small" muted className="text-xs">
                    {check.description}
                  </Text>
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
