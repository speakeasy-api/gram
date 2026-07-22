import { type ReactNode } from "react";
import { ClaudeCodeIcon, CodexIcon, CursorIcon } from "../hooks/HookSourceIcon";

type ProviderIcon = (props: { className?: string }) => JSX.Element;

export type AIIntegrationProvider = {
  provider: string;
  name: string;
  description: string;
  schedules?: AIIntegrationSchedule[];
  onboardingDescription: string;
  setupGuide?: {
    steps: Array<{
      title: string;
      description?: string;
      screenshot?: {
        src: string;
        alt: string;
        caption?: string;
      };
      helpLink?: {
        url: string;
        linkLabel: string;
        sentence: string;
      };
      showsForm?: boolean;
    }>;
  };
  icon: ProviderIcon;
  apiKeyLabel: string;
  apiKeyPlaceholder: string;
  requiresOrganizationId: boolean;
  organizationIdLabel?: string;
  organizationIdPlaceholder?: string;
  helpText?: ReactNode;
};

type AIIntegrationScheduleKind = "events" | "metrics";

export type AIIntegrationSchedule = {
  schedule: string;
  name: string;
  description: string;
  cadence: string;
  // Whether this stream imports discrete events or aggregated metrics.
  kind: AIIntegrationScheduleKind;
  // Fallback stream identifier, shown before the backend has a sync row for
  // the schedule. Once connected, listSchedules returns the authoritative
  // identifier from the backend registry (streamForSchedule in
  // server/internal/aiintegrations/store.go); keep the two in sync.
  signal: string;
  // Where the imported data surfaces in the dashboard. `path` is relative to
  // the current project (e.g. "agent-sessions").
  destination?: {
    label: string;
    path: string;
  };
};

// providerSchedules returns the data products a provider imports, falling
// back to a single generic usage schedule for providers that predate
// per-product metadata.
export function providerSchedules(
  provider: AIIntegrationProvider,
): AIIntegrationSchedule[] {
  return (
    provider.schedules ?? [
      {
        schedule: provider.provider,
        name: "Usage polling",
        description: "Imports provider usage and cost data.",
        cadence: "Hourly",
        kind: "metrics",
        signal: `${provider.provider}.usage`,
      },
    ]
  );
}

const CURSOR_AI_INTEGRATION: AIIntegrationProvider = {
  provider: "cursor",
  name: "Cursor",
  description: "Track Cursor usage and spend across your organization.",
  schedules: [
    {
      schedule: "cursor",
      name: "Usage and spend",
      description: "Imports Cursor usage and cost data.",
      cadence: "Hourly",
      kind: "metrics",
      signal: "cursor.usage",
      destination: { label: "Employee insights", path: "employees" },
    },
  ],
  onboardingDescription:
    "Connect Cursor's Admin API so the platform can import organization-level usage and spend.",
  setupGuide: {
    steps: [
      {
        title: "Open Cursor Team API keys",
        description:
          "Log in to the Cursor dashboard as a team admin and open the Team API keys section.",
        helpLink: {
          url: "https://cursor.com/dashboard/api?section=team-keys#team-api-keys",
          linkLabel: "Cursor Team API keys",
          sentence: "Open {LINK} to create a team API key",
        },
      },
      {
        title: "Create a read-only API key",
        description:
          'Click "New API Key", give the key a recognizable name, set the scope to "Read-only", then save it.',
        screenshot: {
          src: "/setup/cursor-usage-api-key.png",
          alt: "Cursor dashboard API page showing the Team API key creation form with read-only scope selected.",
          caption:
            'Use the Team tab, click "New API Key", set Scope to "Read-only", then save.',
        },
      },
      {
        title: "Paste the key into Speakeasy",
        description:
          "Copy the generated Cursor Admin API key and paste it below. The platform stores it securely and starts importing usage data.",
        showsForm: true,
      },
    ],
  },
  icon: CursorIcon,
  apiKeyLabel: "Cursor Admin API key",
  apiKeyPlaceholder: "key_xxx",
  requiresOrganizationId: false,
};

const ANTHROPIC_AI_INTEGRATION: AIIntegrationProvider = {
  provider: "anthropic_compliance",
  name: "Anthropic Compliance",
  description:
    "Import Claude Chats from claude.ai web and desktop for security review.",
  schedules: [
    {
      schedule: "anthropic_compliance",
      name: "Compliance activity feed",
      description: "Imports Claude Chat activity and messages for review.",
      cadence: "Every 10m",
      kind: "events",
      signal: "claude.chat.message",
      destination: { label: "Agent Sessions", path: "agent-sessions" },
    },
    {
      schedule: "anthropic_analytics_usage",
      name: "Claude Chat usage metrics",
      description: "Imports token and request metrics from Admin Analytics.",
      cadence: "Hourly",
      kind: "metrics",
      signal: "claude.chat.usage.tokens",
      destination: { label: "Employee insights", path: "employees" },
    },
    {
      schedule: "anthropic_analytics_cost",
      name: "Claude Chat cost metrics",
      description: "Imports spend data from Admin Analytics.",
      cadence: "Hourly",
      kind: "metrics",
      signal: "claude.chat.cost.usd",
      destination: { label: "Costs", path: "costs" },
    },
  ],
  onboardingDescription:
    "Connect Anthropic's Compliance API so the platform can import Claude activity for review workflows.",
  setupGuide: {
    steps: [
      {
        title: "Copy your Compliance Access Key",
        description:
          "Create a Compliance Access Key and copy it. Select the read scopes shown below: compliance activities, org data, org settings, user data, analytics, and spend limits. Leave the delete and write scopes unchecked.",
        screenshot: {
          src: "/setup/anthropic-compliance-api-key.png",
          alt: "Create API key dialog showing compliance, analytics, and spend limit read scopes selected.",
          caption:
            "Select read:compliance_activities, read:compliance_org_data, read:compliance_org_settings, read:compliance_user_data, read:analytics, and read:spend_limits.",
        },
        helpLink: {
          url: "https://claude.ai/admin-settings/api-access",
          linkLabel: "Claude API access settings",
          sentence:
            "Open {LINK}, click Create Key, then select the scopes shown below",
        },
      },
      {
        title: "Copy your Anthropic Organization ID",
        description:
          "Log in to Claude, open Organization settings, then scroll to the bottom and copy the Organization ID for the Claude organization whose compliance data Speakeasy should import.",
        screenshot: {
          src: "/setup/anthropic-organization-id.png",
          alt: "Claude Organization settings page showing the Organization ID near the bottom of the page.",
          caption:
            "Open Claude Organization settings, scroll to the Organization section, and copy the Organization ID.",
        },
        helpLink: {
          url: "https://claude.ai/admin-settings/organization",
          linkLabel: "Claude Organization settings",
          sentence: "Open {LINK} to find your Organization ID",
        },
      },
      {
        title: "Paste both values into Speakeasy",
        description:
          "Copy the scoped Compliance Access API key and your Anthropic Organization ID, then paste both below. The platform stores the key securely and starts importing compliance activity, analytics, and spend limit data.",
        showsForm: true,
      },
    ],
  },
  icon: ClaudeCodeIcon,
  apiKeyLabel: "Compliance Access API Key",
  apiKeyPlaceholder: "Paste your Compliance Access Key",
  requiresOrganizationId: true,
  organizationIdLabel: "Anthropic Organization ID",
  organizationIdPlaceholder: "org_xxx",
  helpText: (
    <>
      The API key must include{" "}
      <code className="text-foreground">read:compliance_activities</code>,{" "}
      <code className="text-foreground">read:compliance_org_data</code>,{" "}
      <code className="text-foreground">read:compliance_org_settings</code>,{" "}
      <code className="text-foreground">read:compliance_user_data</code>,{" "}
      <code className="text-foreground">read:analytics</code>, and{" "}
      <code className="text-foreground">read:spend_limits</code>. Leave{" "}
      <code className="text-foreground">delete:compliance_user_data</code> and{" "}
      <code className="text-foreground">write:spend_limits</code> unchecked.{" "}
      <a
        href="https://platform.claude.com/docs/en/manage-claude/compliance-api-access"
        target="_blank"
        rel="noopener noreferrer"
        className="text-foreground underline underline-offset-4"
      >
        Learn more
      </a>
      .
    </>
  ),
};

const CODEX_AI_INTEGRATION: AIIntegrationProvider = {
  provider: "codex_compliance",
  name: "OpenAI Compliance Logs",
  description: "Import Codex usage and spend from OpenAI Compliance Logs.",
  schedules: [
    {
      schedule: "codex_compliance",
      name: "Codex cost metrics",
      description: "Imports Codex spend from Compliance Logs COSTS files.",
      cadence: "Hourly",
      kind: "metrics",
      signal: "codex.cost.usd",
      destination: { label: "Costs", path: "costs" },
    },
  ],
  onboardingDescription:
    "Connect OpenAI's Compliance Logs API so the platform can import Codex cost data for reporting.",
  setupGuide: {
    steps: [
      {
        title: "Create a Compliance API key",
        description:
          "Create an OpenAI API key with access to compliance logs for the organization whose Codex spend Speakeasy should import.",
      },
      {
        title: "Copy your OpenAI organization ID",
        description:
          "Copy the OpenAI organization ID for the organization whose Codex cost logs Speakeasy should import. It should start with org-.",
      },
      {
        title: "Paste both values into Speakeasy",
        description:
          "Copy the Compliance API key and OpenAI organization ID, then paste both below. The platform stores the key securely and starts importing Codex cost logs.",
        showsForm: true,
      },
    ],
  },
  icon: CodexIcon,
  apiKeyLabel: "OpenAI Compliance Logs API key",
  apiKeyPlaceholder: "Paste your OpenAI Compliance Logs API key",
  requiresOrganizationId: true,
  organizationIdLabel: "OpenAI organization ID",
  organizationIdPlaceholder: "org-...",
  helpText: (
    <>
      Codex cost import uses OpenAI Compliance Logs Platform{" "}
      <code className="text-foreground">COSTS</code> files for an OpenAI API
      organization. Use an <code className="text-foreground">org-*</code> ID.
    </>
  ),
};

export const AI_INTEGRATION_PROVIDERS: AIIntegrationProvider[] = [
  CURSOR_AI_INTEGRATION,
  ANTHROPIC_AI_INTEGRATION,
  CODEX_AI_INTEGRATION,
];
