import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import type { SetResourceAudienceEntry } from "@gram/client/models/components/setresourceaudienceentry.js";
import { ownRules } from "../mcp/access/serverAudience";
import { withRule } from "../mcp/access/manageAccessState";

/** Add connect exclusions without rewriting inherited or unrelated grants. */
export function assignmentExclusions(
  entries: ResourceAudienceEntry[],
  principals: string[],
): SetResourceAudienceEntry[] {
  return principals.reduce<SetResourceAudienceEntry[]>(
    (result, principalUrn) => {
      result = withRule(result, principalUrn, "blocked");
      return result;
    },
    ownRules(entries).map(({ principalUrn, level, tools, dispositions }) => ({
      principalUrn,
      level,
      tools,
      dispositions,
    })),
  );
}
