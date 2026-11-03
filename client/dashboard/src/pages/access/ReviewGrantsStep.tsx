import { Badge } from "@/components/ui/Badge";
import { Checkbox } from "@/components/ui/Checkbox";
import { Text } from "@/components/ui/Text";

import type { AddedPermission } from "./reviewGrants";

/**
 * What a role save adds, shown before the save goes through. Every added
 * permission is listed with what it carries along; the confirmation is
 * required only when one of them is sensitive.
 */
export function ReviewGrantsStep({
  added,
  holdersNote,
  confirmed,
  onConfirmedChange,
}: {
  added: AddedPermission[];
  /** Who is affected, e.g. "6 members hold this role today." */
  holdersNote?: string;
  confirmed: boolean;
  onConfirmedChange: (confirmed: boolean) => void;
}): JSX.Element {
  const sensitive = added.filter((p) => p.sensitiveVia.length > 0);

  return (
    <div className="space-y-4">
      <div className="space-y-1.5">
        <Text muted small>
          Added {added.length === 1 ? "permission" : "permissions"} ·{" "}
          {added.length}
        </Text>
        <ul className="border-border divide-border divide-y border">
          {added.map((permission) => (
            <li key={permission.scope} className="space-y-0.5 px-3 py-2">
              <div className="flex flex-wrap items-center gap-2">
                <Text variant="body" className="text-sm font-medium">
                  {permission.label}
                </Text>
                <span className="text-muted-foreground font-mono text-xs">
                  {permission.scope}
                </span>
                {permission.sensitiveVia.length > 0 && (
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
              {permission.includes.length > 0 && (
                <Text muted small>
                  Includes {permission.includes.join(", ")}.
                </Text>
              )}
            </li>
          ))}
        </ul>
      </div>

      <Text muted small>
        {sensitive.length > 0 &&
          `${sensitive.length} of these ${sensitive.length === 1 ? "exposes" : "expose"} other members' activity or ${sensitive.length === 1 ? "allows" : "allow"} destructive changes. `}
        {holdersNote}
      </Text>

      {sensitive.length > 0 && (
        <label className="flex cursor-pointer items-start gap-3 text-left">
          <Checkbox
            checked={confirmed}
            onCheckedChange={(checked) => onConfirmedChange(checked === true)}
            aria-label="Confirm sensitive permissions"
            className="mt-0.5"
          />
          <Text variant="body" className="text-sm">
            I reviewed the sensitive permissions above and want this role to
            grant them.
          </Text>
        </label>
      )}
    </div>
  );
}
