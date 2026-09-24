import type { OnboardingTaskId } from "./tasks";

// Dependency payloads contain keys rather than the task titles shown on the board.
const DEPENDENCY_TITLES: Record<OnboardingTaskId, string> = {
  "domain-verification": "Verify your domain",
  "identity-provider": "Set up identity provider",
  "connect-idp": "Connect identity provider",
  "directory-sync": "Set up directory sync",
  "enable-logging": "Enable logging",
  "anthropic-observability": "Set up Anthropic observability",
  "anthropic-admin-controls": "Set up Anthropic admin controls",
  "create-marketplace": "Create marketplace",
  "instrument-agents": "Set up observability in other platforms",
  litellm: "Set up LiteLLM",
  "additional-agent-config": "Configure integrations",
  "confirm-traffic": "Confirm traffic",
  "distribute-servers": "Distribute MCP servers",
  "configure-policies": "Configure policies",
  "platform-mcp": "Set up Platform MCP",
};

export function dependencyTitle(key: string): string {
  if (Object.hasOwn(DEPENDENCY_TITLES, key)) {
    return DEPENDENCY_TITLES[key as OnboardingTaskId];
  }
  const words = key.replace(/[-_]+/g, " ").trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}
