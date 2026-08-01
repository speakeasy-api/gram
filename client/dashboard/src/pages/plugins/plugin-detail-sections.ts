// Anchor ids for the plugin detail page sections. The plugin detail sidebar
// nav (plugin-detail-sidebar-nav.tsx) links to these via `#<id>`, and the page
// scrolls the matching section into view — mirroring the skill detail page's
// hash-driven section navigation. Kept in their own module so the sidebar nav
// and the page can share them without importing each other.
export const PLUGIN_OVERVIEW_SECTION_ID = "overview";
export const PLUGIN_SERVERS_SECTION_ID = "servers";
export const PLUGIN_SKILLS_SECTION_ID = "skills";
export const PLUGIN_ASSIGNMENTS_SECTION_ID = "assignments";
export const PLUGIN_SETTINGS_SECTION_ID = "settings";

export const PLUGIN_SECTION_IDS: readonly string[] = [
  PLUGIN_OVERVIEW_SECTION_ID,
  PLUGIN_SERVERS_SECTION_ID,
  PLUGIN_SKILLS_SECTION_ID,
  PLUGIN_ASSIGNMENTS_SECTION_ID,
  PLUGIN_SETTINGS_SECTION_ID,
];
