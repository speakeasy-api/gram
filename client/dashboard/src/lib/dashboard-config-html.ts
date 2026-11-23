// The index.html side of the dashboard runtime configuration (see
// runtime-config.ts). Kept free of DOM and library imports because
// vite.config.ts imports it too.
export const DASHBOARD_CONFIG_META_NAME = "gram-dashboard-config";
export const DASHBOARD_CONFIG_PLACEHOLDER = "${GRAM_DASHBOARD_CONFIG}";

/**
 * Substitutes the config placeholder in index.html for the dev server, which
 * has no container entrypoint. Escapes the value the same way the entrypoint
 * (42-dashboard-config.sh) does, because it lands in an attribute.
 */
export function replaceDashboardConfig(html: string, value: string): string {
  const escaped = value
    .replaceAll("&", "&amp;")
    .replaceAll('"', "&quot;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
  return html.replace(DASHBOARD_CONFIG_PLACEHOLDER, () => escaped);
}
