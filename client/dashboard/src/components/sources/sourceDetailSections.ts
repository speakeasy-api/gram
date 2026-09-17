const SOURCE_DETAIL_TABS = [
  "overview",
  "tools",
  "versions",
  "settings",
] as const;

export type SourceDetailTab = (typeof SOURCE_DETAIL_TABS)[number];

export const DEFAULT_SOURCE_DETAIL_TAB: SourceDetailTab = "overview";

// The page has been addressed by hash through two layouts, and those links
// are still in docs, chat history and the CLI's output. Each old name lands
// on the tab that holds its content now.
const LEGACY_HASH_TABS = new Map<string, SourceDetailTab>([
  ["details", "overview"],
  ["activity", "overview"],
  ["content", "overview"],
  ["spec", "overview"],
  ["mcp-servers", "overview"],
  ["deployments", "versions"],
]);

export function tabForHash(hash: string): SourceDetailTab | null {
  const id = hash.replace(/^#/, "");
  if (id === "") return null;
  if ((SOURCE_DETAIL_TABS as readonly string[]).includes(id)) {
    return id as SourceDetailTab;
  }
  return LEGACY_HASH_TABS.get(id) ?? null;
}
