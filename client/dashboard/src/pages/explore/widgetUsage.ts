import type { WidgetDashboard } from "@gram/client/models/components/widgetdashboard.js";

/** How many dashboards to name before counting the rest. */
const NAMED = 3;

/**
 * The dashboards a widget is on, as a phrase: "Agent activity", "Agent
 * activity and Costs", or "Agent activity, Costs, Latency and 2 more
 * dashboards".
 */
export function describeDashboards(dashboards: WidgetDashboard[]): string {
  const names = dashboards.map((dashboard) => `“${dashboard.name}”`);
  if (names.length <= NAMED) {
    if (names.length <= 1) return names[0] ?? "";
    return `${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;
  }
  const rest = names.length - NAMED;
  return `${names.slice(0, NAMED).join(", ")} and ${rest} more ${rest === 1 ? "dashboard" : "dashboards"}`;
}
