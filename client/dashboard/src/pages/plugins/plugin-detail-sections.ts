import type { useRoutes } from "@/routes";

// The plugin detail sections are distinct subpages under
// /plugins/:pluginId/<section>. The page component renders one at a time and
// the sidebar nav links between them, so both derive the active section from
// the pathname via this module.
export const PLUGIN_OVERVIEW_SECTION_ID = "overview";
export const PLUGIN_SERVERS_SECTION_ID = "servers";
export const PLUGIN_SKILLS_SECTION_ID = "skills";
export const PLUGIN_ASSIGNMENTS_SECTION_ID = "assignments";
export const PLUGIN_SETTINGS_SECTION_ID = "settings";

const SECTIONS = [
  PLUGIN_OVERVIEW_SECTION_ID,
  PLUGIN_SERVERS_SECTION_ID,
  PLUGIN_SKILLS_SECTION_ID,
  PLUGIN_ASSIGNMENTS_SECTION_ID,
  PLUGIN_SETTINGS_SECTION_ID,
] as const;

export type PluginSection = (typeof SECTIONS)[number];

function isPluginSection(value: string | undefined): value is PluginSection {
  return !!value && (SECTIONS as readonly string[]).includes(value);
}

// The section named by the path segment after :pluginId, defaulting to overview
// so the bare /plugins/:pluginId URL still resolves.
export function activePluginSection(
  pathname: string,
  pluginId: string | undefined,
): PluginSection {
  if (!pluginId) return PLUGIN_OVERVIEW_SECTION_ID;
  const segments = pathname.split("/").filter(Boolean);
  const pluginIndex = segments.lastIndexOf(pluginId);
  const segment = pluginIndex === -1 ? undefined : segments[pluginIndex + 1];
  return isPluginSection(segment) ? segment : PLUGIN_OVERVIEW_SECTION_ID;
}

export function pluginSectionHref(
  routes: ReturnType<typeof useRoutes>,
  pluginId: string,
  section: PluginSection,
): string {
  switch (section) {
    case PLUGIN_OVERVIEW_SECTION_ID:
      return routes.plugins.detail.overview.href(pluginId);
    case PLUGIN_SERVERS_SECTION_ID:
      return routes.plugins.detail.servers.href(pluginId);
    case PLUGIN_SKILLS_SECTION_ID:
      return routes.plugins.detail.skills.href(pluginId);
    case PLUGIN_ASSIGNMENTS_SECTION_ID:
      return routes.plugins.detail.assignments.href(pluginId);
    case PLUGIN_SETTINGS_SECTION_ID:
      return routes.plugins.detail.settings.href(pluginId);
  }
}
