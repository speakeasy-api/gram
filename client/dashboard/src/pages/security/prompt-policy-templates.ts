export type PromptPolicyTemplate = { name: string; prompt: string };

export const PROMPT_POLICY_TEMPLATES: PromptPolicyTemplate[] = [
  {
    name: "No production deletes",
    prompt:
      "Any tool call that performs a destructive operation (DELETE, DROP, TRUNCATE) against a production resource.",
  },
  {
    name: "Data exfiltration",
    prompt:
      "Tool-call sequences where sensitive data is read and then transmitted to an external destination.",
  },
  {
    name: "MCP tool allowlist",
    prompt:
      "Tool calls should use explicitly approved MCP servers; unapproved servers or tools are policy violations.",
  },
  {
    name: "PII exposure",
    prompt: "Tool calls that expose personally identifiable information.",
  },
];
