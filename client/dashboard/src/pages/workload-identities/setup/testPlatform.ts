import type { WorkloadPlatform } from "@gram/client/models/components/workloadplatform.js";

function block(
  fields: Partial<WorkloadPlatform["steps"][number]["blocks"][number]> & {
    type: WorkloadPlatform["steps"][number]["blocks"][number]["type"];
  },
): WorkloadPlatform["steps"][number]["blocks"][number] {
  return {
    markdown: "",
    src: "",
    alt: "",
    caption: "",
    href: "",
    label: "",
    variable: "",
    value: "",
    help: "",
    ...fields,
  };
}

/**
 * A catalog platform as the API returns it, shaped like the server's
 * claude-tag.yaml, for tests.
 */
export const testPlatform: WorkloadPlatform = {
  key: "claude-tag",
  displayName: "Claude Tag",
  description: "Let Claude in Slack call your MCP servers.",
  icon: "/access-hub/claude-tag.svg",
  enabled: true,
  issuer: {
    value: "https://identity.anthropic.com/agents",
    visibility: "hidden",
  },
  jwksUri: {
    value: "https://identity.anthropic.com/agents/jwks.json",
    visibility: "hidden",
  },
  variables: [
    {
      key: "org_id",
      tier: "rule",
      label: "Anthropic organization ID",
      help: "",
      placeholder: "",
      pattern: "[A-Za-z0-9-]+",
      patternMessage: "An organization ID is letters, numbers and hyphens.",
    },
  ],
  subject: {
    template: "wimse://identity.anthropic.com/org/{org_id}/agent/",
    wildcard: true,
  },
  steps: [
    {
      id: "before",
      title: "Before you start",
      phase: "collect",
      blocks: [block({ type: "text", markdown: "You need access." })],
    },
    {
      id: "organization",
      title: "Your Anthropic organization",
      phase: "collect",
      blocks: [
        block({ type: "field", variable: "org_id" }),
        block({ type: "subject_rule" }),
      ],
    },
    {
      id: "agent",
      title: "Choose an agent and connect",
      phase: "create",
      blocks: [block({ type: "agent_picker" }), block({ type: "tags" })],
    },
    {
      id: "console",
      title: "Values for the Anthropic console",
      phase: "connect",
      blocks: [
        block({ type: "computed_status" }),
        block({
          type: "computed",
          value: "token_endpoint",
          label: "Token endpoint",
        }),
        block({
          type: "computed",
          value: "issuer_url",
          label: "Issuer URL",
          help: "Leave the field empty if this shows nothing.",
        }),
        block({
          type: "computed",
          value: "mcp_host",
          label: "Allowed API hosts",
        }),
      ],
    },
    {
      id: "check",
      title: "Check it worked",
      phase: "connect",
      blocks: [block({ type: "text", markdown: "Ask Claude." })],
    },
  ],
};
