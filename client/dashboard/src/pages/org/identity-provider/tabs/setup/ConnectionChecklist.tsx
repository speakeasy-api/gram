import { useEffect, useRef, useState, type ReactNode } from "react";
import { useLocation } from "react-router";
import { Check, ChevronDown } from "lucide-react";

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Badge } from "@/components/ui/Badge";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type {
  IdentityProviderConnectionChecklistItem,
  IdentityProviderConnectionChecklistItemKey,
} from "@gram/client/models/components/identityproviderconnectionchecklistitem.js";

import { AGENT_SECTION_ID } from "../../tabs";
import {
  activeChecklistGroup,
  groupChecklist,
  type ChecklistGroup,
  type ChecklistGroupId,
  type LiveConnection,
} from "../../connectionView";

export type StepAffordances = Partial<
  Record<
    IdentityProviderConnectionChecklistItemKey,
    (connection: LiveConnection) => ReactNode
  >
>;

function Step({
  item,
  index,
  affordance,
}: {
  item: IdentityProviderConnectionChecklistItem;
  index: number;
  affordance?: ReactNode;
}): JSX.Element {
  const done = item.completed === true;
  return (
    <li
      className="flex gap-4 py-4 first:pt-0 last:pb-0"
      data-completed={done ? "true" : undefined}
    >
      <span
        className={cn(
          "flex h-5 w-6 shrink-0 items-center font-mono text-xs leading-5",
          done ? "text-success-foreground opacity-60" : "text-muted-foreground",
        )}
        aria-hidden="true"
      >
        {done ? (
          <Check className="h-4 w-4" />
        ) : (
          String(index + 1).padStart(2, "0")
        )}
      </span>
      <div className="flex min-w-0 flex-col gap-1.5">
        <div className={cn("flex flex-col gap-1.5", done && "opacity-60")}>
          <div className="flex flex-wrap items-center gap-2">
            <Text className={cn("font-medium", done && "line-through")}>
              {item.title}
              {done && <span className="sr-only"> (done)</span>}
            </Text>
            {!done && (
              <Badge variant={item.completed === false ? "warning" : "neutral"}>
                {item.completed === false ? "Needs attention" : "Not checked"}
              </Badge>
            )}
          </div>
          <Text muted small className="break-words whitespace-pre-line">
            {item.description}
          </Text>
          {item.details.length > 0 && (
            <ol className="list-decimal space-y-1 pl-5">
              {item.details.map((detail) => (
                <li key={detail}>
                  <Text muted small>
                    {detail}
                  </Text>
                </li>
              ))}
            </ol>
          )}
        </div>
        {affordance}
      </div>
    </li>
  );
}

function GroupSection({
  group,
  open,
  onOpenChange,
  connection,
  affordances,
}: {
  group: ChecklistGroup;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  connection: LiveConnection;
  affordances: StepAffordances;
}): JSX.Element {
  const complete = group.completedCount === group.items.length;
  const summary = `${group.completedCount} of ${group.items.length} complete`;
  return (
    <Collapsible open={open} onOpenChange={onOpenChange}>
      <CollapsibleTrigger
        className="flex w-full items-start justify-between gap-4 py-1 text-left"
        aria-label={`${group.title}, ${summary}`}
      >
        <div className="flex min-w-0 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-3">
            <span className="text-eyebrow">{group.title}</span>
            <Text muted small>
              {summary}
            </Text>
          </div>
          <Text muted small>
            {group.id === "connect" && complete
              ? "Okta connection and required access verified."
              : group.description}
          </Text>
        </div>
        <ChevronDown
          className={cn(
            "text-muted-foreground mt-1 h-4 w-4 shrink-0 transition-transform",
            open && "rotate-180",
          )}
          aria-hidden="true"
        />
      </CollapsibleTrigger>
      <CollapsibleContent forceMount hidden={!open}>
        <ol className="mt-4 divide-y border-t pt-4">
          {group.items.map((item, index) => (
            <Step
              key={item.key}
              item={item}
              index={index}
              affordance={affordances[item.key]?.(connection)}
            />
          ))}
        </ol>
      </CollapsibleContent>
    </Collapsible>
  );
}

/** The phase the connection is in is expanded until the admin toggles a group; a later #agent navigation wins again. */
export function ConnectionChecklist({
  connection,
  affordances = {},
}: {
  connection: LiveConnection;
  affordances?: StepAffordances;
}): JSX.Element {
  const groups = groupChecklist(connection.checklist);
  const location = useLocation();
  const checklistRef = useRef<HTMLDivElement>(null);
  const agentHash = `#${AGENT_SECTION_ID}`;
  const [override, setOverride] = useState<{
    group: ChecklistGroupId | null;
    locationKey: string;
  }>();
  const agentRequested =
    location.hash === agentHash && override?.locationKey !== location.key;
  let expanded = activeChecklistGroup(connection);
  if (agentRequested) expanded = "cross_app_access";
  else if (override) expanded = override.group;

  useEffect(() => {
    if (location.hash === agentHash) {
      checklistRef.current
        ?.querySelector(agentHash)
        ?.scrollIntoView({ block: "start" });
    }
  }, [location.hash, location.key, agentHash]);

  return (
    <div ref={checklistRef} className="flex flex-col gap-8">
      {groups.map((group) => (
        <GroupSection
          key={group.id}
          group={group}
          open={expanded === group.id}
          onOpenChange={(open) =>
            setOverride({
              group: open ? group.id : null,
              locationKey: location.key,
            })
          }
          connection={connection}
          affordances={affordances}
        />
      ))}
    </div>
  );
}
