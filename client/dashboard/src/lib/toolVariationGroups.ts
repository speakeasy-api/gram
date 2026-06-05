// The backend auto-provisions a single project-wide tool variations group under
// this exact name (see variations.InitGlobalToolVariationsGroup). It's an
// internal implementation detail, so the UI surfaces a friendlier label for it
// without renaming the underlying record. Shared by the MCP Settings tab
// (group selector) and the Tools tab (resolved-group label) so the override
// stays consistent.
export const GLOBAL_TOOL_VARIATIONS_GROUP_BACKEND_NAME =
  "Global tool variations";
export const GLOBAL_TOOL_VARIATIONS_GROUP_DISPLAY_NAME = "All source tool tags";

export function toolVariationsGroupDisplayName(name: string): string {
  return name === GLOBAL_TOOL_VARIATIONS_GROUP_BACKEND_NAME
    ? GLOBAL_TOOL_VARIATIONS_GROUP_DISPLAY_NAME
    : name;
}
