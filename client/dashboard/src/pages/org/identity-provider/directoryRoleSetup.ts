import type { ApplyDirectoryRoleSetupForm } from "@gram/client/models/components/applydirectoryrolesetupform.js";
import type { ApplyDirectoryRoleSetupResult } from "@gram/client/models/components/applydirectoryrolesetupresult.js";
import type { DirectoryRoleSetupPreviewItem } from "@gram/client/models/components/directoryrolesetuppreviewitem.js";
import type { DirectoryRoleSetupPreviewResult } from "@gram/client/models/components/directoryrolesetuppreviewresult.js";
import type { DirectoryRoleSetupSource } from "@gram/client/models/components/directoryrolesetupsource.js";
import type { RoleGrant } from "@gram/client/models/components/rolegrant.js";

import { pluralize } from "@/lib/format";

/** Which role a source gets: an existing one, or a new one by name. */
export type SetupChoice =
  | { kind: "reuse"; roleUrn: string }
  | { kind: "create"; name: string };

/** One unmapped source in the bulk setup, with the role it will get. */
export type SetupRow = {
  key: string;
  item: DirectoryRoleSetupPreviewItem;
  /** What the preview proposed, kept so the row can offer it again. */
  proposed: SetupChoice;
  choice: SetupChoice;
  selected: boolean;
};

/** The most sources one apply takes; the server refuses more. */
export const MAX_SETUP_ITEMS = 500;

function setupSourceKey(source: DirectoryRoleSetupSource): string {
  return source.sourceKind === "group"
    ? `group:${source.directoryGroupId ?? ""}`
    : `attribute:${JSON.stringify([source.attributeKey ?? "", source.attributeValue ?? ""])}`;
}

export function choiceValue(choice: SetupChoice): string {
  return choice.kind === "reuse"
    ? `reuse:${choice.roleUrn}`
    : `create:${choice.name}`;
}

export function choiceFromValue(value: string): SetupChoice {
  return value.startsWith("reuse:")
    ? { kind: "reuse", roleUrn: value.slice("reuse:".length) }
    : { kind: "create", name: value.slice("create:".length) };
}

function proposedChoice(
  item: DirectoryRoleSetupPreviewItem,
): SetupChoice | undefined {
  const role = item.proposedRoles[0];
  if (!role) return undefined;
  if (role.roleAction === "reuse" && role.roleUrn) {
    return { kind: "reuse", roleUrn: role.roleUrn };
  }
  return { kind: "create", name: role.roleName };
}

/**
 * The choice a row keeps when the preview is refreshed. A choice the admin
 * never changed follows the refreshed proposal, so a role someone else
 * created meanwhile is reused rather than created again. A choice the admin
 * made is kept, unless it creates a role the refreshed preview now reuses by
 * that name: that create can only conflict again.
 */
function refreshedChoice(
  earlier: SetupRow | undefined,
  proposed: SetupChoice,
): SetupChoice {
  if (!earlier) return proposed;
  if (choiceValue(earlier.choice) === choiceValue(earlier.proposed)) {
    return proposed;
  }
  if (
    earlier.choice.kind === "create" &&
    proposed.kind === "reuse" &&
    choiceValue(earlier.proposed) !== choiceValue(proposed)
  ) {
    return proposed;
  }
  return earlier.choice;
}

/**
 * The rows to offer: every source with no roles yet. Sources that already
 * have roles are edited in the mappings table instead, so bulk setup never
 * replaces a role set someone chose. Groups start selected; attribute values
 * start cleared, since a key can have many values and each would get a role.
 *
 * Rows already on screen keep their selection when the preview is refreshed,
 * as long as the source is still unmapped. See refreshedChoice for the role.
 */
export function setupRows(
  preview: DirectoryRoleSetupPreviewResult,
  previous: SetupRow[] = [],
): SetupRow[] {
  const kept = new Map(previous.map((row) => [row.key, row]));
  const rows: SetupRow[] = [];
  for (const item of preview.items) {
    if (item.existingRoleUrns.length > 0) continue;
    const proposed = proposedChoice(item);
    if (!proposed) continue;
    const key = setupSourceKey(item.source);
    const earlier = kept.get(key);
    rows.push({
      key,
      item,
      proposed,
      choice: refreshedChoice(earlier, proposed),
      selected: earlier?.selected ?? item.source.sourceKind === "group",
    });
  }
  return rows.sort(
    (a, b) =>
      (a.item.source.sourceKind === "group" ? 0 : 1) -
        (b.item.source.sourceKind === "group" ? 0 : 1) ||
      a.item.sourceName.localeCompare(b.item.sourceName),
  );
}

/** Names of the new roles the selected rows create, once each. */
export function newRoleNames(rows: SetupRow[]): string[] {
  const names = new Map<string, string>();
  for (const row of rows) {
    if (!row.selected || row.choice.kind !== "create") continue;
    const key = row.choice.name.trim().toLowerCase();
    if (!names.has(key)) names.set(key, row.choice.name);
  }
  return [...names.values()];
}

/** Existing roles the selected rows reuse, once each. */
export function reusedRoleUrns(rows: SetupRow[]): string[] {
  return [
    ...new Set(
      rows.flatMap((row) =>
        row.selected && row.choice.kind === "reuse" ? [row.choice.roleUrn] : [],
      ),
    ),
  ];
}

/** The apply request for the selected rows. */
export function setupRequest(
  rows: SetupRow[],
  grantSet?: RoleGrant[],
): ApplyDirectoryRoleSetupForm {
  return {
    items: rows
      .filter((row) => row.selected)
      .map((row) => ({
        source: row.item.source,
        version: row.item.version,
        roles: [
          row.choice.kind === "reuse"
            ? { roleUrn: row.choice.roleUrn }
            : { createName: row.choice.name },
        ],
      })),
    // Only roles this request creates take the grant set, so leave it out
    // when there are none.
    grantSet: grantSet && newRoleNames(rows).length > 0 ? grantSet : undefined,
  };
}

/**
 * What applying did. Syncing new roles to the identity provider runs after
 * the response, so it is reported as requested, never as done.
 */
export function setupResultSummary(
  result: ApplyDirectoryRoleSetupResult,
  sourceCount: number,
): string {
  const parts = [`Mapped ${pluralize(sourceCount, "source")}.`];
  if (result.createdRoles.length > 0) {
    parts.push(`Created ${pluralize(result.createdRoles.length, "role")}.`);
  }
  const syncs = result.sideEffects.filter(
    (effect) =>
      effect.kind === "workos_role_sync" &&
      effect.requestStatus === "requested",
  ).length;
  if (syncs > 0) {
    parts.push(
      `Syncing ${pluralize(syncs, "new role")} to WorkOS has been requested and can take a few minutes.`,
    );
  }
  return parts.join(" ");
}
