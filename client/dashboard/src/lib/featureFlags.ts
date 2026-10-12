export const FEATURE_FLAGS = {
  agentManagement: "agent-management",
  agentCredentials: "agent-identity-credentials",
  assistants: "assistants",
  budgets: "gram-budgets",
  deploymentsPage: "gram-deployments-page",
  deviceAgent: "gram-device-agent",
  deviceIntegrations: "gram-device-integrations",
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
  // UI-only rollout gate for folding Shadow AI into Security and Policy: the
  // Shadow AI policy kind in Guardrails, the Watchdog decision tile and the
  // retired Shadow AI nav entry. Not enforced server-side.
  shadowAiGuardrails: "gram-shadow-ai-guardrails",
  tunneledMcp: "gram-tunneled-mcp",
  userSessionsDashboard: "user-sessions-dashboard",
} as const;

export type FeatureFlag = (typeof FEATURE_FLAGS)[keyof typeof FEATURE_FLAGS];
