import { type ReactNode } from "react";
import { ClaudeCodeIcon, CursorIcon } from "../hooks/HookSourceIcon";

type ProviderIcon = (props: { className?: string }) => JSX.Element;

export type AIIntegrationProvider = {
  provider: string;
  name: string;
  description: string;
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

export const AI_INTEGRATION_PROVIDERS: AIIntegrationProvider[] = [
  {
    provider: "cursor",
    name: "Cursor",
    description: "Track Cursor usage and spend across your organization.",
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
            "Copy the generated Cursor Admin API key and paste it below. Gram stores it securely and starts importing usage data.",
          showsForm: true,
        },
      ],
    },
    icon: CursorIcon,
    apiKeyLabel: "Cursor Admin API key",
    apiKeyPlaceholder: "key_xxx",
    requiresOrganizationId: false,
  },
  {
    provider: "anthropic_compliance",
    name: "Anthropic Compliance",
    description:
      "Import Claude Chats from claude.ai web and desktop for security review.",
    onboardingDescription:
      "Connect Anthropic's Compliance API so Gram can import Claude activity for review workflows.",
    setupGuide: {
      steps: [
        {
          title: "Copy your Compliance Access Key",
          description:
            "Create a Compliance Access Key and copy it. Use a Compliance Access Key, not an Admin API key: Admin API keys only reach the Activity Feed, while Compliance Access Keys can access the broader compliance data Speakeasy imports.",
          helpLink: {
            url: "https://platform.claude.com/docs/en/manage-claude/compliance-api-access",
            linkLabel: "Compliance API access guide",
            sentence: "Follow {LINK} to provision the key",
          },
        },
        {
          title: "Copy your Anthropic Organization ID",
          description:
            "Log in to Claude, open Organization settings, then scroll to the bottom and copy the Organization ID for the Claude organization whose compliance data Gram should import.",
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
            "Copy the Compliance Access API key and your Anthropic Organization ID, then paste both below. The platform stores the key securely and starts importing compliance activity.",
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
        The API key must include the{" "}
        <code className="text-foreground">read:compliance_activities</code> and{" "}
        <code className="text-foreground">read:compliance_user_data</code>{" "}
        scopes.{" "}
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
  },
];
