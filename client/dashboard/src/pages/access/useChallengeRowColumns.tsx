import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { useSession } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import type { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { Column } from "@speakeasy-api/moonshine";
import { KeyRound } from "lucide-react";
import { useMemo } from "react";
import { OutcomeBadge } from "./ChallengesTab";
import { ResourceLink } from "./ResourceLink";
import { getInitials, reasonLabel } from "./challengeHelpers";

export function useChallengeRowColumns(
  animatingOutIds?: Set<string>,
  groupCounts?: Map<string, number>,
  groupKeys?: Map<string, string>,
  onToggleGroup?: (groupKey: string) => void,
  outcomeFilter?: string,
): Column<AuthzChallenge>[] {
  const { orgSlug } = useSlugs();
  const { organization } = useSession();
  const { data: membersData } = useMembers();
  const projectMap = useMemo(() => {
    const m = new Map<string, { slug: string; name: string }>();
    for (const p of organization.projects) {
      m.set(p.id, { slug: p.slug, name: p.name });
    }
    return m;
  }, [organization.projects]);
  const memberMap = useMemo(() => {
    const m = new Map<string, { email: string; photoUrl?: string }>();
    for (const member of membersData?.members ?? []) {
      m.set(member.id, { email: member.email, photoUrl: member.photoUrl });
    }
    return m;
  }, [membersData]);

  return useMemo(() => {
    const rowFade = (row: AuthzChallenge) =>
      animatingOutIds?.has(row.id)
        ? "opacity-0 transition-opacity duration-1000"
        : outcomeFilter === "deny" &&
          (row.outcome === "allow" || row.resolvedAt) &&
          "opacity-40 transition-opacity duration-1000";

    return [
      {
        key: "avatar",
        header: "",
        width: "40px",
        render: (row: AuthzChallenge) => {
          const isApiKey = row.principalType === "api_key";
          const display = row.userEmail ?? row.principalUrn;
          return (
            <div className={cn(rowFade(row))}>
              <Avatar className="h-8 w-8">
                {row.photoUrl && (
                  <AvatarImage src={row.photoUrl} alt={display} />
                )}
                <AvatarFallback className="text-[11px]">
                  {isApiKey ? (
                    <KeyRound className="h-4 w-4" />
                  ) : (
                    getInitials(display)
                  )}
                </AvatarFallback>
              </Avatar>
            </div>
          );
        },
      },
      {
        key: "identity",
        header: "Identity",
        width: "1.2fr",
        render: (row: AuthzChallenge) => (
          <Tooltip>
            <TooltipTrigger asChild>
              <Type
                variant="body"
                className={cn(
                  "min-w-0 truncate text-sm font-medium",
                  rowFade(row),
                )}
              >
                {row.userEmail ?? row.principalUrn}
              </Type>
            </TooltipTrigger>
            {row.roleSlugs.length > 0 && (
              <TooltipContent side="bottom">
                Roles: {row.roleSlugs.join(", ")}
              </TooltipContent>
            )}
          </Tooltip>
        ),
      },
      {
        key: "outcome",
        header: "Outcome",
        width: "80px",
        render: (row: AuthzChallenge) => (
          <div className={cn(rowFade(row))}>
            <OutcomeBadge outcome={row.outcome} resolved={!!row.resolvedAt} />
          </div>
        ),
      },
      {
        key: "scope",
        header: "Required Scope",
        width: "1fr",
        render: (row: AuthzChallenge) => (
          <Tooltip>
            <TooltipTrigger asChild>
              <code
                className={cn(
                  "bg-muted min-w-0 truncate rounded px-1.5 py-0.5 font-mono text-xs",
                  rowFade(row),
                )}
              >
                {row.scope}
              </code>
            </TooltipTrigger>
            <TooltipContent side="bottom" className="max-w-xs">
              <p className="text-xs">
                {reasonLabel(row.reason)}
                {row.evaluatedGrantCount > 0 &&
                  ` (${row.matchedGrantCount} of ${row.evaluatedGrantCount} grants matched)`}
              </p>
            </TooltipContent>
          </Tooltip>
        ),
      },
      {
        key: "resource",
        header: "Resource",
        width: "1.2fr",
        render: (row: AuthzChallenge) => (
          <div className={cn("min-w-0 truncate", rowFade(row))}>
            <ResourceLink
              challenge={row}
              orgSlug={orgSlug ?? ""}
              projectMap={projectMap}
            />
          </div>
        ),
      },
      {
        key: "resolvedBy",
        header: "Resolved By",
        width: "100px",
        render: (row: AuthzChallenge) => {
          if (!row.resolvedBy) {
            return (
              <Type variant="body" className="text-muted-foreground/40 text-sm">
                —
              </Type>
            );
          }
          const userId = row.resolvedBy.replace(/^user:/, "");
          const member = memberMap.get(userId);
          const display = member?.email ?? row.resolvedBy;
          return (
            <Tooltip>
              <TooltipTrigger asChild>
                <Avatar className="h-7 w-7">
                  {member?.photoUrl && (
                    <AvatarImage src={member.photoUrl} alt={display} />
                  )}
                  <AvatarFallback className="text-[10px]">
                    {getInitials(display)}
                  </AvatarFallback>
                </Avatar>
              </TooltipTrigger>
              <TooltipContent>{display}</TooltipContent>
            </Tooltip>
          );
        },
      },
      {
        key: "timestamp",
        header: "Time",
        width: "1fr",
        render: (row: AuthzChallenge) => {
          const count = groupCounts?.get(row.id) ?? 1;
          return (
            <div className={cn("flex items-center gap-1.5", rowFade(row))}>
              <Tooltip delayDuration={500}>
                <TooltipTrigger asChild>
                  <Type
                    variant="body"
                    className="text-muted-foreground cursor-default text-sm whitespace-nowrap underline decoration-dotted underline-offset-4"
                  >
                    <HumanizeDateTime date={row.timestamp} />
                  </Type>
                </TooltipTrigger>
                <TooltipContent>
                  {row.timestamp.toLocaleString()}
                </TooltipContent>
              </Tooltip>
              {count > 1 && (
                <button
                  type="button"
                  onClick={() => {
                    const key = groupKeys?.get(row.id);
                    if (key) onToggleGroup?.(key);
                  }}
                  className="text-muted-foreground bg-muted hover:bg-primary/10 hover:text-primary cursor-pointer rounded-full px-1.5 py-0.5 text-[10px] font-medium tabular-nums transition-colors"
                >
                  ×{count}
                </button>
              )}
            </div>
          );
        },
      },
    ];
  }, [
    orgSlug,
    projectMap,
    memberMap,
    animatingOutIds,
    groupCounts,
    groupKeys,
    onToggleGroup,
    outcomeFilter,
  ]);
}
