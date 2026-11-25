import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import {
  SetResourceAudienceEntryDispositions$outboundSchema,
  SetResourceAudienceEntryLevel$outboundSchema,
  type SetResourceAudienceEntry,
} from "@gram/client/models/components/setresourceaudienceentry.js";
import { ownRules } from "../mcp/access/serverAudience";
import { withRule } from "../mcp/access/manageAccessState";

/** Validate returned annotations before preserving them in a replacement write. */
export function assignmentWriteEntry({
  principalUrn,
  level,
  tools,
  dispositions,
}: ResourceAudienceEntry): SetResourceAudienceEntry {
  return {
    principalUrn,
    level: SetResourceAudienceEntryLevel$outboundSchema.parse(level),
    tools,
    dispositions: dispositions?.map((disposition) =>
      SetResourceAudienceEntryDispositions$outboundSchema.parse(disposition),
    ),
  };
}

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
    ownRules(entries).map(assignmentWriteEntry),
  );
}
