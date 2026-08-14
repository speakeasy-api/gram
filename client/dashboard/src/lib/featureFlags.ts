export const FEATURE_FLAGS = {
  assistants: "assistants",
  budgets: "gram-budgets",
  deploymentsPage: "gram-deployments-page",
  deviceAgent: "gram-device-agent",
  deviceIntegrations: "gram-device-integrations",
  enterpriseTrials: "enterprise-trials",
  experimentalChat: "gram-experimental-chat",
  externalMcpUserSessions: "onboard-external-mcp-to-user-sessions",
  functions: "gram-functions",
  newCostsPage: "gram-new-costs-page",
  platformMcp: "platform-mcp",
  platformMcpDashboard: "platform-mcp-dashboard",
  promptPolicies: "gram-prompt-policies",
  rbac: "gram-rbac",
  riskWatchdog: "gram-risk-watchdog",
  tunneledMcp: "gram-tunneled-mcp",
  userSessionsDashboard: "user-sessions-dashboard",
} as const;

export type FeatureFlag = (typeof FEATURE_FLAGS)[keyof typeof FEATURE_FLAGS];
