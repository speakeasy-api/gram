import type { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";
import { useMemo, useRef } from "react";

function challengeGroupKey(c: AuthzChallenge): string {
  const displayIdentity = c.userEmail ?? c.principalUrn;
  return `${displayIdentity}|${c.scope}|${c.outcome}|${c.resourceKind ?? ""}|${c.resourceId ?? ""}`;
}

interface ChallengeGroupsResult {
  grouped: AuthzChallenge[];
  groupCounts: Map<string, number>;
  groupKeys: Map<string, string>;
  /** Ref populated after each grouping — read via getGroupChallengeIds callback. */
  groupSiblingIdsRef: React.RefObject<Map<string, string[]>>;
}

export function useChallengeGroups(
  challenges: AuthzChallenge[],
  expandedGroups?: Set<string>,
): ChallengeGroupsResult {
  const groupSiblingIdsRef = useRef<Map<string, string[]>>(new Map());

  const { grouped, groupCounts, groupKeys } = useMemo(() => {
    const groups = new Map<string, AuthzChallenge[]>();
    for (const c of challenges) {
      const key = challengeGroupKey(c);
      const arr = groups.get(key);
      if (arr) arr.push(c);
      else groups.set(key, [c]);
    }

    const result: AuthzChallenge[] = [];
    const counts = new Map<string, number>();
    const keys = new Map<string, string>();
    const siblingIds = new Map<string, string[]>();
    for (const [key, members] of groups) {
      const memberIds = members.map((m) => m.id);
      counts.set(members[0].id, members.length);
      for (const m of members) {
        keys.set(m.id, key);
        siblingIds.set(m.id, memberIds);
      }
      if (expandedGroups?.has(key)) {
        result.push(...members);
      } else {
        result.push(members[0]);
      }
    }
    groupSiblingIdsRef.current = siblingIds;
    return { grouped: result, groupCounts: counts, groupKeys: keys };
  }, [challenges, expandedGroups]);

  return { grouped, groupCounts, groupKeys, groupSiblingIdsRef };
}
