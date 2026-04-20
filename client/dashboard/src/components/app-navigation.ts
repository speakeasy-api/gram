import type { IconName } from "@speakeasy-api/moonshine";

export type AppNavRouteKey =
  | "home"
  | "sources"
  | "catalog"
  | "playground"
  | "skills"
  | "elements"
  | "mcp"
  | "slackApps"
  | "observability"
  | "logs"
  | "chatSessions"
  | "hooks"
  | "settings";

export const APP_NAV_GROUPS = {
  project: ["home"],
  connect: ["sources", "catalog", "playground"],
  build: ["skills", "elements", "mcp", "slackApps"],
  observe: ["observability", "logs", "chatSessions", "hooks"],
  settings: ["settings"],
} satisfies Record<string, AppNavRouteKey[]>;

export const APP_LOADING_NAV_META: Record<
  AppNavRouteKey,
  { label: string; icon: IconName }
> = {
  home: { label: "Home", icon: "house" },
  sources: { label: "Sources", icon: "file-code" },
  catalog: { label: "Catalog", icon: "store" },
  playground: { label: "Playground", icon: "message-circle" },
  skills: { label: "Skills", icon: "sparkles" },
  elements: { label: "Chat Elements", icon: "message-circle" },
  mcp: { label: "MCP", icon: "network" },
  slackApps: { label: "Assistants", icon: "bot" },

  observability: { label: "Insights", icon: "layout-dashboard" },
  logs: { label: "MCP Logs", icon: "file-text" },
  chatSessions: { label: "Agent Sessions", icon: "messages-square" },
  hooks: { label: "Hooks", icon: "webhook" },
  settings: { label: "Settings", icon: "settings" },
};
