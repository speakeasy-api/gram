/**
 * The areas an MCP server or gateway settings tab is grouped into, in page
 * order. Each id anchors its area; none reuses a section's own id, so deep
 * links to sections keep landing on the section.
 */
export const SETTINGS_GROUPS = {
  general: { id: "settings-general", title: "General" },
  access: { id: "settings-access", title: "Access" },
  identity: { id: "settings-sign-in", title: "Sign-in and identity" },
  connection: { id: "settings-connection", title: "Connection" },
  tools: { id: "settings-tools", title: "Tools" },
  instructions: { id: "settings-instructions", title: "Agent instructions" },
  danger: { id: "settings-danger-zone", title: "Danger zone" },
} as const;

export const SETTINGS_GROUP_IDS: readonly string[] = Object.values(
  SETTINGS_GROUPS,
).map((group) => group.id);
