const SOURCE_DETAIL_SECTION_IDS = [
  "details",
  "activity",
  "tools",
  "content",
  "versions",
  "settings",
] as const;

export type SourceDetailSectionId = (typeof SOURCE_DETAIL_SECTION_IDS)[number];

// The tabbed page this one replaced was addressed by hash, and those links
// are still in docs, chat history and the CLI's output. Each old tab lands on
// the section that took over its content.
const LEGACY_HASH_SECTIONS = new Map<string, SourceDetailSectionId>([
  ["overview", "details"],
  ["mcp-servers", "details"],
  ["spec", "content"],
  ["deployments", "versions"],
]);

export function sectionIdForHash(hash: string): SourceDetailSectionId | null {
  const id = hash.replace(/^#/, "");
  if (id === "") return null;
  if ((SOURCE_DETAIL_SECTION_IDS as readonly string[]).includes(id)) {
    return id as SourceDetailSectionId;
  }
  return LEGACY_HASH_SECTIONS.get(id) ?? null;
}
