/**
 * Wording for the acting surface recorded on every audit log entry.
 *
 * The server stores a closed set of short identifiers and passes them through
 * unlabelled, the way it does for actions, so the phrasing lives here alone
 * rather than being split between the API and this file.
 */
const ACTING_SURFACE_LABELS: Record<string, string> = {
  dashboard: "Dashboard",
  api_key: "API key",
  platform_mcp: "Platform MCP",
  project_assistant: "Project assistant",
  unknown: "Unknown",
};

/**
 * Human wording for one acting surface. An unrecognized value is titled from
 * its own identifier rather than hidden: a surface the server has learned to
 * record and this build has not yet been taught about is still worth showing.
 */
export function formatActingSurfaceLabel(surface: string): string {
  return (
    ACTING_SURFACE_LABELS[surface] ??
    surface
      .split("_")
      .map((part) => (part ? part[0]!.toUpperCase() + part.slice(1) : part))
      .join(" ")
  );
}

/**
 * Whether a surface is worth calling out on a row.
 *
 * The dashboard is the assumed surface for work done in the dashboard, so
 * labelling every dashboard row adds noise to the common case without adding
 * information. An unknown surface is equally unhelpful to display: it means
 * nothing was recorded, which the absence of a badge already conveys.
 */
export function isNotableActingSurface(surface: string): boolean {
  return surface !== "dashboard" && surface !== "unknown" && surface !== "";
}
