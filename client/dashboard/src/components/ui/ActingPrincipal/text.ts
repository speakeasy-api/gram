import type { IdentityRef } from "@/lib/identity-urn";

/** A person an agent acted for. */
export type Principal = {
  id: string;
  name?: string;
  photoUrl?: string;
  /**
   * The reference their identity page is keyed on, for a caller that holds
   * one. `id` is not it: a chat participant is keyed by the provider handle
   * the room reported ("slack:U123"), which names no identity here, so a
   * person links only when the caller says which identity they are.
   */
  identity?: IdentityRef;
};

/**
 * The same relation `ActingPrincipal` renders, as plain text, for a tooltip,
 * a title attribute or a CSV cell.
 *
 * Lives beside the component rather than inside it so that file exports only
 * components: a module that also exports a plain function loses fast refresh,
 * and every edit to it remounts the tree instead of patching it.
 */
export function actingPrincipalText(
  agent: { id: string; name?: string },
  authorizer?: Principal | Principal[],
): string {
  const agentLabel = agent.name || `agent:${agent.id}`;
  const people = authorizer
    ? Array.isArray(authorizer)
      ? authorizer
      : [authorizer]
    : [];
  if (people.length === 0) return agentLabel;
  return `${agentLabel} on behalf of ${people
    .map((person) => person.name || person.id)
    .join(", ")}`;
}
