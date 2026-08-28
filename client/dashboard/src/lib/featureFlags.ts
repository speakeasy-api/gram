export const FEATURE_FLAGS = {
  assistants: "assistants",
  budgets: "gram-budgets",
  deploymentsPage: "gram-deployments-page",
  deviceAgent: "gram-device-agent",
  deviceIntegrations: "gram-device-integrations",
  experimentalChat: "gram-experimental-chat",
  functions: "gram-functions",
  gatewayEndpoints: "gram-gateway-endpoints",
  headlessModeSwitcher: "headless-mode-switcher",
  mcpResearch: "gram-mcp-research",
  newCostsPage: "gram-new-costs-page",
  paygSelfServeBilling: "gram-payg-self-serve-billing",
  platformMcp: "platform-mcp",
  platformMcpDashboard: "platform-mcp-dashboard",
  promptPolicies: "gram-prompt-policies",
  rbac: "gram-rbac",
  riskWatchdog: "gram-risk-watchdog",
  tunneledMcp: "gram-tunneled-mcp",
  userSessionsDashboard: "user-sessions-dashboard",
} as const;

export type FeatureFlag = (typeof FEATURE_FLAGS)[keyof typeof FEATURE_FLAGS];
