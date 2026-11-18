import { Badge } from "@/components/ui/Badge";
import { Text } from "@/components/ui/Text";

import { ConfirmSensitiveAccess } from "./ConfirmSensitiveAccess";
import {
  reviewNeedsConfirmation,
  type AddedPermission,
  type NewHolders,
} from "./reviewGrants";

function describeHolders(holders: NewHolders): string {
  const parts: string[] = [];
  if (holders.members > 0) {
    parts.push(
      `${holders.members} new ${holders.members === 1 ? "member" : "members"}`,
    );
  }
  if (holders.agents > 0) {
    parts.push(
      `${holders.agents} new ${holders.agents === 1 ? "agent" : "agents"}`,
    );
  }
  return parts.join(" and ");
}

/**
 * What a role save gives, shown before the save goes through. Every added
 * permission is listed with what it carries along, and so is the sensitive
 * access new holders of the role gain; the confirmation is required only
 * when something sensitive is among them.
 */
export function ReviewGrantsStep({
  added,
  newHolders,
  holdersNote,
  confirmed,
  onConfirmedChange,
}: {
  added: AddedPermission[];
  newHolders?: NewHolders | null;
  /** Who is affected, e.g. "6 members hold this role today." */
  holdersNote?: string;
  confirmed: boolean;
  onConfirmedChange: (confirmed: boolean) => void;
}): JSX.Element {
  const sensitive = added.filter((p) => p.sensitive);
  const holdersGain = newHolders?.sensitive ?? [];

  return (
    <div className="space-y-4">
      {added.length > 0 && (
        <div className="space-y-1.5">
          <Text muted small>
            Added {added.length === 1 ? "permission" : "permissions"} ·{" "}
            {added.length}
          </Text>
          <PermissionList permissions={added} />
        </div>
      )}

      {newHolders && holdersGain.length > 0 && (
        <div className="space-y-1.5">
          <Text muted small>
            Sensitive access for {describeHolders(newHolders)} ·{" "}
            {holdersGain.length}
          </Text>
          <PermissionList permissions={holdersGain} />
        </div>
      )}

      <Text muted small>
        {sensitive.length > 0 &&
          `${sensitive.length} of these ${sensitive.length === 1 ? "exposes" : "expose"} other members' activity or ${sensitive.length === 1 ? "allows" : "allow"} destructive changes. `}
        {holdersNote}
      </Text>

      {reviewNeedsConfirmation(added, newHolders ?? null) && (
        <ConfirmSensitiveAccess
          checked={confirmed}
          onCheckedChange={onConfirmedChange}
        >
          I reviewed the sensitive permissions above and want this role to grant
          them.
        </ConfirmSensitiveAccess>
      )}
    </div>
  );
}

function PermissionList({
  permissions,
}: {
  permissions: AddedPermission[];
}): JSX.Element {
  return (
    <ul className="border-border divide-border divide-y border">
      {permissions.map((permission) => (
        <li key={permission.scope} className="space-y-0.5 px-3 py-2">
          <div className="flex flex-wrap items-center gap-2">
            <Text variant="body" className="text-sm font-medium">
              {permission.label}
            </Text>
            <span className="text-muted-foreground font-mono text-xs">
              {permission.scope}
            </span>
            {permission.sensitive && (
              <Badge variant="warning" size="sm">
                Sensitive
              </Badge>
            )}
            <Badge variant="neutral" size="sm">
              {permission.places === null
                ? "Everywhere"
                : `${permission.places} ${permission.places === 1 ? "place" : "places"}`}
            </Badge>
          </div>
          {permission.description && (
            <Text muted small>
              {permission.description}
            </Text>
          )}
          {permission.sensitiveVia.length > 0 && (
            <Text muted small>
              Sensitive because it includes {permission.sensitiveVia.join(", ")}
              .
            </Text>
          )}
          {permission.includes.length > 0 && (
            <Text muted small>
              Includes {permission.includes.join(", ")}.
            </Text>
          )}
        </li>
      ))}
    </ul>
  );
}
