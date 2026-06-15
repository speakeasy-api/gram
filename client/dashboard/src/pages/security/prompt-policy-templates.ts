export type PromptPolicyTemplate = { name: string; prompt: string };

export const PROMPT_POLICY_TEMPLATES: PromptPolicyTemplate[] = [
  {
    name: "No production deletes",
    prompt:
      "Any tool call that performs a destructive operation (DELETE, DROP, TRUNCATE) against a production resource.",
  },
  {
    name: "External data transfer",
    prompt:
      "A tool call that sends data to an external or third-party destination, such as an outbound network request, email, or file upload.",
  },
  {
    name: "PII exposure",
    prompt: "Tool calls that expose personally identifiable information.",
  },
];
