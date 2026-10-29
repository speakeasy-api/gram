import type { RoleDirectorySource } from "@/pages/access/directorySourcesForRole";

/** How many live sources a role row names before folding the rest into "+N more". */
const SHOWN_SOURCES = 2;

export interface RoleDirectorySourceText {
  /** The one-line summary under the role's name. */
  line: string;
  /** Every source, live ones first, for the tooltip. */
  full: string;
}

function plural(count: number, noun: string): string {
  return `${count} ${noun}${count === 1 ? "" : "s"}`;
}

/**
 * The text of a role row's directory line. The line names the first few live
 * groups and attribute values that give out the role, folds the rest into a
 * count, and counts removed sources rather than naming them, so it stays on
 * one line. Undefined when nothing maps the role.
 */
export function roleDirectorySourceText(
  sources: RoleDirectorySource[],
): RoleDirectorySourceText | undefined {
  if (sources.length === 0) return undefined;
  const live = sources.filter((source) => !source.missing);
  const missing = sources.filter((source) => source.missing);

  const parts: string[] = [];
  if (live.length > 0) {
    const named = live
      .slice(0, SHOWN_SOURCES)
      .map((source) => source.label)
      .join(", ");
    const hidden = live.length - SHOWN_SOURCES;
    parts.push(hidden > 0 ? `${named} +${hidden} more` : named);
  }
  if (missing.length > 0) {
    parts.push(plural(missing.length, "removed source"));
  }

  return {
    line: `Via directory: ${parts.join(" · ")}`,
    full: `Via directory: ${[...live, ...missing].map((source) => source.label).join(", ")}`,
  };
}
