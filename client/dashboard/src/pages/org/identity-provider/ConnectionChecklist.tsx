import { useEffect, useRef, useState } from "react";
import { useLocation } from "react-router";
import { Check, ChevronDown } from "lucide-react";

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Badge } from "@/components/ui/Badge";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { IdentityProviderConnectionChecklistItem } from "@gram/client/models/components/identityproviderconnectionchecklistitem.js";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";

import { AGENT_SECTION_ID, AgentSetupForm } from "./OktaConnectionSteps";
import {
  activeChecklistGroup,
  groupChecklist,
  oktaAdminConsoleUrl,
  type ChecklistGroup,
  type ChecklistGroupId,
} from "./connectionView";

/** Actions and inputs that belong to individual checklist steps. */
function StepAffordance({
  item,
  connection,
}: {
  item: IdentityProviderConnectionChecklistItem;
  connection: OktaIdentityProviderConnection;
}): JSX.Element | null {
  switch (item.key) {
    case "create_api_services_app":
    case "add_oin_app":
      return (
        <a
          href={oktaAdminConsoleUrl(connection.orgUrl)}
          target="_blank"
          rel="noopener noreferrer"
          className="text-sm underline underline-offset-4"
        >
          Open the Okta Admin Console
        </a>
      );
    case "public_key_auth":
      return (
        <span className="inline-flex max-w-full items-center gap-2 font-mono text-xs">
          <span className="min-w-0 [overflow-wrap:anywhere]">
            {connection.jwksUrl}
          </span>
          <CopyButton
            text={connection.jwksUrl}
            size="xs"
            tooltip="Copy public key URL (JWKS)"
          />
        </span>
      );
    case "create_ai_agent":
      return connection.status === "revoked" ? null : (
        <AgentSetupForm
          key={`${connection.agentId ?? ""}|${connection.agentAppId ?? ""}`}
          connection={connection}
        />
      );
    default:
      return null;
  }
}

function Step({
  item,
  index,
  connection,
}: {
  item: IdentityProviderConnectionChecklistItem;
  index: number;
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  const done = item.completed === true;
  return (
    <li
      className={cn(
        "flex gap-4 py-4 first:pt-0 last:pb-0",
        done && "opacity-60",
      )}
      data-completed={done ? "true" : undefined}
    >
      <span
        className={cn(
          "flex h-5 w-6 shrink-0 items-center font-mono text-xs leading-5",
          done ? "text-success-foreground" : "text-muted-foreground",
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
        <StepAffordance item={item} connection={connection} />
      </div>
    </li>
  );
}

function GroupSection({
  group,
  open,
  onOpenChange,
  connection,
}: {
  group: ChecklistGroup;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  connection: OktaIdentityProviderConnection;
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
      <CollapsibleContent
        forceMount={group.id === "cross_app_access" ? true : undefined}
        hidden={!open}
      >
        <ol className="mt-4 divide-y border-t pt-4">
          {group.items.map((item, index) => (
            <Step
              key={item.key}
              item={item}
              index={index}
              connection={connection}
            />
          ))}
        </ol>
      </CollapsibleContent>
    </Collapsible>
  );
}

/** The console checklist as two phases; the phase the connection is in starts expanded. */
export function ConnectionChecklist({
  connection,
}: {
  connection: OktaIdentityProviderConnection;
}): JSX.Element {
  const groups = groupChecklist(connection.checklist);
  const location = useLocation();
  const checklistRef = useRef<HTMLDivElement>(null);
  const agentHash = `#${AGENT_SECTION_ID}`;
  const [expanded, setExpanded] = useState<ChecklistGroupId | null>(
    location.hash === agentHash
      ? "cross_app_access"
      : activeChecklistGroup(connection),
  );
  useEffect(() => {
    if (location.hash === agentHash) setExpanded("cross_app_access");
  }, [location.hash, location.key, agentHash]);
  useEffect(() => {
    if (location.hash === agentHash && expanded === "cross_app_access") {
      checklistRef.current
        ?.querySelector(agentHash)
        ?.scrollIntoView({ block: "start" });
    }
  }, [location.hash, location.key, agentHash, expanded]);
  return (
    <div ref={checklistRef} className="flex flex-col gap-8">
      {groups.map((group) => (
        <GroupSection
          key={group.id}
          group={group}
          open={expanded === group.id}
          onOpenChange={(open) => setExpanded(open ? group.id : null)}
          connection={connection}
        />
      ))}
    </div>
  );
}
