export const FEATURE_FLAGS = {
  assistants: "assistants",
  budgets: "gram-budgets",
  deploymentsPage: "gram-deployments-page",
  deviceAgent: "gram-device-agent",
  deviceIntegrations: "gram-device-integrations",
  // UI-only mirror of the backend `ema-providers` rollout, which the API enforces.
  emaProviders: "ema-providers",
  experimentalChat: "gram-experimental-chat",
  explore: "gram-explore",
  functions: "gram-functions",
  gatewayEndpoints: "gram-gateway-endpoints",
  headlessModeSwitcher: "headless-mode-switcher",
  killswitches: "gram-killswitches",
  mcpResearch: "gram-mcp-research",
  // UI-only rollout gate for the MCP scope picker in the risk policy editor;
  // not enforced server-side.
  mcpScopedPolicies: "gram-mcp-scoped-policies",
  newCostsPage: "gram-new-costs-page",
  oktaConnections: "okta-connections",
  paygSelfServeBilling: "gram-payg-self-serve-billing",
  promptPolicies: "gram-prompt-policies",
  rbac: "gram-rbac",
  // Multivariate (`off` | `shadow` | `llm`), org-targeted: which risk engine
  // the organization runs. Read through `useFeatureFlagVariant`; the boolean
  // read is true for every variant, `off` included. `useDetectorMode` maps it
  // to the policy editor mode.
  riskLlmAnalyzer: "gram-risk-llm-analyzer",
  riskWatchdog: "gram-risk-watchdog",
  tunneledMcp: "gram-tunneled-mcp",
  // UI-only rollout gate for the organization's Speakeasy authorization
  // servers page; the APIs behind it are not gated.
  userSessionIssuerDashboard: "gram-user-session-issuer-dashboard",
  userSessionsDashboard: "user-sessions-dashboard",
} as const;

export type FeatureFlag = (typeof FEATURE_FLAGS)[keyof typeof FEATURE_FLAGS];
