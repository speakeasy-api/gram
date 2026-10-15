import {
  createContext,
  use,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type Dispatch,
  type ReactNode,
  type SetStateAction,
} from "react";
import { useLocation } from "react-router";
import { Check, ChevronDown } from "lucide-react";

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Badge } from "@/components/ui/Badge";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
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

/** Steps whose details must be followed in order; every other list is a set. */
const ORDERED_DETAIL_KEYS: ReadonlySet<IdentityProviderConnectionChecklistItemKey> =
  new Set(["public_key_auth"]);

function DetailList({
  ordered,
  children,
}: {
  ordered: boolean;
  children: ReactNode;
}): JSX.Element {
  return ordered ? (
    <ol className="list-decimal space-y-1 pl-5">{children}</ol>
  ) : (
    <ul className="list-disc space-y-1 pl-5">{children}</ul>
  );
}

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
            {item.completed === false && (
              <Badge variant="warning">Needs attention</Badge>
            )}
            {item.completed === undefined && (
              <SimpleTooltip tooltip="Speakeasy has no evidence for this step. Check it yourself in Okta.">
                <span tabIndex={0} className="inline-flex">
                  <Badge variant="neutral">Not checked</Badge>
                </span>
              </SimpleTooltip>
            )}
          </div>
          <Text muted small className="break-words whitespace-pre-line">
            {item.description}
          </Text>
          {item.details.length > 0 && (
            <DetailList ordered={ORDERED_DETAIL_KEYS.has(item.key)}>
              {item.details.map((detail) => (
                <li key={detail}>
                  <Text muted small>
                    {detail}
                  </Text>
                </li>
              ))}
            </DetailList>
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
  const description = complete ? group.completeDescription : group.description;
  const descriptionId = useId();
  return (
    <Collapsible open={open} onOpenChange={onOpenChange}>
      <h4>
        <CollapsibleTrigger
          className="flex w-full items-center justify-between gap-4 py-1 text-left"
          aria-label={`${group.title}, ${summary}`}
          aria-describedby={description ? descriptionId : undefined}
        >
          <span className="flex min-w-0 flex-wrap items-center gap-3">
            {complete && (
              <Check
                className="text-success-foreground h-4 w-4 shrink-0"
                aria-hidden="true"
              />
            )}
            <span className="text-eyebrow text-default font-semibold">
              {group.title}
            </span>
            <span className="text-muted-foreground text-sm">{summary}</span>
          </span>
          <ChevronDown
            className={cn(
              "text-muted-foreground h-4 w-4 shrink-0 transition-transform",
              open && "rotate-180",
            )}
            aria-hidden="true"
          />
        </CollapsibleTrigger>
      </h4>
      {description && (
        <Text id={descriptionId} muted small className="mt-1 max-w-3xl">
          {description}
        </Text>
      )}
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

type Override = {
  group: ChecklistGroupId | null;
  locationKey: string;
  /** The group expanded by default when the admin toggled; a different default retires the override. */
  defaultGroup: ChecklistGroupId | null;
};
type OverridesState = [
  Record<string, Override>,
  Dispatch<SetStateAction<Record<string, Override>>>,
];

const ChecklistOverridesContext = createContext<OverridesState | null>(null);

/** Keeps each checklist's expanded or collapsed choice while its tab is unmounted. */
export function ChecklistOverridesProvider({
  children,
}: {
  children: ReactNode;
}): JSX.Element {
  const state = useState<Record<string, Override>>({});
  return (
    <ChecklistOverridesContext.Provider value={state}>
      {children}
    </ChecklistOverridesContext.Provider>
  );
}

function useChecklistOverride(
  scope: string,
): [Override | undefined, (next: Override) => void] {
  const local = useState<Record<string, Override>>({});
  const [overrides, setOverrides] = use(ChecklistOverridesContext) ?? local;
  const setOverride = useCallback(
    (next: Override) =>
      setOverrides((previous) => ({ ...previous, [scope]: next })),
    [scope, setOverrides],
  );
  return [overrides[scope], setOverride];
}

/** The phase the connection is in is expanded until the admin toggles a group; a later #agent navigation wins again. A tab that hides the active phase expands its first incomplete group instead. */
export function ConnectionChecklist({
  connection,
  affordances = {},
  groups: groupIds,
}: {
  connection: LiveConnection;
  affordances?: StepAffordances;
  /** Which groups to show; each tab renders the phase it owns. Defaults to all. */
  groups?: ChecklistGroupId[];
}): JSX.Element {
  const groups = groupChecklist(connection.checklist).filter(
    (group) => groupIds === undefined || groupIds.includes(group.id),
  );
  const location = useLocation();
  const checklistRef = useRef<HTMLDivElement>(null);
  const agentHash = `#${AGENT_SECTION_ID}`;
  const rendered = new Set(groups.map((group) => group.id));
  let defaultGroup = activeChecklistGroup(connection);
  if (defaultGroup !== null && !rendered.has(defaultGroup)) {
    defaultGroup =
      groups.find((group) => group.completedCount < group.items.length)?.id ??
      null;
  }
  const [stored, setOverride] = useChecklistOverride(
    groupIds?.join(",") ?? "all",
  );
  const override = stored?.defaultGroup === defaultGroup ? stored : undefined;
  const agentRequested =
    location.hash === agentHash &&
    rendered.has("cross_app_access") &&
    override?.locationKey !== location.key;
  let expanded = defaultGroup;
  if (agentRequested) expanded = "cross_app_access";
  else if (override) expanded = override.group;

  useEffect(() => {
    if (location.hash === agentHash) {
      checklistRef.current
        ?.querySelector(agentHash)
        ?.scrollIntoView({ block: "start" });
    }
  }, [location.hash, location.key, agentHash]);

  // Record the #agent expansion so it survives a tab switch like a manual toggle.
  useEffect(() => {
    if (agentRequested) {
      setOverride({
        group: "cross_app_access",
        locationKey: location.key,
        defaultGroup,
      });
    }
  }, [agentRequested, location.key, defaultGroup, setOverride]);

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
              defaultGroup,
            })
          }
          connection={connection}
          affordances={affordances}
        />
      ))}
    </div>
  );
}
