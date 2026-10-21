import type { RoleDirectorySource } from "@/pages/access/directorySourcesForRole";

/**
 * The text of a role row's directory line: the groups and attribute values
 * that give out the role, live ones first. Undefined when nothing maps it.
 */
export function roleDirectorySourceText(
  sources: RoleDirectorySource[],
): string | undefined {
  if (sources.length === 0) return undefined;
  const ordered = [
    ...sources.filter((source) => !source.missing),
    ...sources.filter((source) => source.missing),
  ];
  return `Via directory: ${ordered.map((source) => source.label).join(", ")}`;
}
