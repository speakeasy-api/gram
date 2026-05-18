export type OnboardingStep =
  | "connect-idp"
  | "directory-sync"
  | "instrument-agents"
  | "add-sources"
  | "confirm-traffic";

export interface StepConfig {
  id: OnboardingStep;
  title: string;
  description: string;
  icon: string;
}

export const ONBOARDING_STEPS: StepConfig[] = [
  {
    id: "connect-idp",
    title: "Connect Identity Provider",
    description: "Link your SSO provider",
    icon: "shield",
  },
  {
    id: "directory-sync",
    title: "Directory Sync",
    description: "Confirm roles and admins",
    icon: "users",
  },
  {
    id: "instrument-agents",
    title: "Instrument Agents",
    description: "Setup agent platform hooks",
    icon: "cpu",
  },
  {
    id: "add-sources",
    title: "Add MCP Sources",
    description: "Configure 1st & 3rd party sources",
    icon: "database",
  },
  {
    id: "confirm-traffic",
    title: "Confirm Traffic",
    description: "Verify compliance",
    icon: "activity",
  },
];

// Mock data types
export interface IdpProvider {
  id: string;
  name: string;
  icon: string;
  type: string;
  connected: boolean;
}

export interface DirectoryUser {
  id: string;
  name: string;
  email: string;
  role: "admin" | "member";
  avatarUrl?: string;
}

export interface AgentPlatform {
  id: string;
  name: string;
  description: string;
  icon: string;
  connected: boolean;
}

export interface McpSource {
  id: string;
  name: string;
  type: "1st-party" | "3rd-party";
  description: string;
  enabled: boolean;
}

export interface TrafficMetric {
  label: string;
  value: string;
  trend: "up" | "down" | "stable";
  healthy: boolean;
}
