import { useResourceLinkMaps } from "@/pages/access/useResourceLinkMaps";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { Key } from "@gram/client/models/components/key.js";

import { keyGrants, summarizePermissions } from "./agent-key-permissions";

/**
 * What one key can do, and what the agent now allows that it cannot. The key
 * carries the grants it was issued with; the agent's policy only ever narrows
 * them, so a permission added later needs a new key.
 */
export function AgentKeyPermissions({
  apiKey,
  missing,
}: {
  apiKey: Key;
  missing: AgentPolicyGrant[];
}): JSX.Element {
  const grants = keyGrants(apiKey);
  const names = [...(grants ?? []), ...missing];
  const maps = useResourceLinkMaps(
    names.some(
      ({ selector }) =>
        selector.resourceKind === "mcp" && selector.resourceId !== "*",
    ),
  );
  if (!grants)
    return (
      <p className="text-muted-foreground text-xs">
        Uses the agent&rsquo;s current permissions.
      </p>
    );
  return (
    <div className="space-y-1 text-xs">
      <p className="text-muted-foreground break-words">
        <span className="text-foreground">Can:</span>{" "}
        {summarizePermissions(grants, maps).join(" · ")}
      </p>
      {missing.length > 0 && (
        <p className="text-default-warning break-words">
          <span className="font-medium">Missing:</span>{" "}
          {summarizePermissions(missing, maps).join(" · ")}. Added to the agent
          after this key was issued, so the key cannot use{" "}
          {missing.length === 1 ? "it" : "them"} until you reissue it.
        </p>
      )}
    </div>
  );
}
