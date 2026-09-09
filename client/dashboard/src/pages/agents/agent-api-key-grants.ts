import { z } from "zod";
import { AgentPolicySelector$inboundSchema } from "@gram/client/models/components/agentpolicyselector.js";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";

// The editor uses the API's wire format; parsing also rejects malformed selectors.
export function parseDelegatedGrants(value: string): AgentPolicyGrantForm[] {
  const parsed: unknown = JSON.parse(value);
  if (!Array.isArray(parsed))
    throw new Error("Enter a JSON array of allow grants.");
  return parsed.map((grant: unknown) => {
    const form = z
      .object({
        effect: z.literal("allow"),
        scope: z.string().min(1),
        selector: z
          .object({
            resource_kind: z.string(),
            resource_id: z.string().min(1),
            disposition: z.string().optional(),
            project_id: z.string().optional(),
            server_identity: z.string().optional(),
            server_url: z.string().optional(),
            tool: z.string().optional(),
          })
          .strict(),
      })
      .strict()
      .parse(grant);
    return {
      ...form,
      selector: AgentPolicySelector$inboundSchema.parse(form.selector),
    };
  });
}

// Match Go strings.TrimSpace and utf8.RuneCountInString used by agent issuance.
export function validateAgentAPIKeyName(value: string): string {
  const name = value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, "");
  if (!name) throw new Error("Enter a key name.");
  for (const prefix of ["plugins-", "litellm-"]) {
    if (name.startsWith(prefix))
      throw new Error(
        `Key names starting with "${prefix}" are reserved. Choose another name.`,
      );
  }
  if (Array.from(name).length > 255)
    throw new Error(
      "Key names must not exceed 255 Unicode characters. Shorten the name.",
    );
  return name;
}
