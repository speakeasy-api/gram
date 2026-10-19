import type { DirectoryReviewItem } from "@gram/client/models/components/directoryreviewitem.js";
import type { ResolveDirectoryReviewItemForm } from "@gram/client/models/components/resolvedirectoryreviewitemform.js";
import type { SetupChoice } from "./directoryRoleSetup";
import { pluralize } from "@/lib/format";

/** What an admin can decide about a review item. */
export type ReviewDecision = "approve" | "dismiss" | "opt_out_key";

/** The headline for an item: what changed in the directory. */
export function reviewItemTitle(item: DirectoryReviewItem): string {
  const source = item.item.source;
  const name = item.item.sourceName;
  switch (item.kind) {
    case "new_group":
      return `New group: ${name}`;
    case "new_attribute_value":
      return `New ${source.attributeKey ?? "attribute"} value: ${source.attributeValue ?? name}`;
    case "source_missing":
      return source.sourceKind === "group"
        ? `Group deleted: ${name}`
        : `No one has ${name} any more`;
  }
}

/** The line under the headline: who it affects, and what deciding does. */
export function reviewItemDetail(
  item: DirectoryReviewItem,
  roleNames: (urns: string[]) => string[],
): string {
  const people = item.item.directoryPeople;
  const peopleText = `${people} ${people === 1 ? "directory person" : "directory people"}`;
  if (item.kind !== "source_missing") {
    return `${peopleText}. Map it to a role, or leave it unmapped.`;
  }
  const names = roleNames(item.item.existingRoleUrns);
  const roles =
    names.length > 0
      ? names.join(", ")
      : pluralize(item.item.existingRoleUrns.length, "role");
  const again =
    item.occurrence > 1 ? " It has come back and gone again before." : "";
  const grant = item.item.existingRoleUrns.length === 1 ? "grants" : "grant";
  return `Still mapped to ${roles}, which ${grant} nothing while it is gone.${again} Remove the roles, or keep them for when it returns.`;
}

/** The request for a decision. Only approving a new source sends a role. */
export function reviewDecisionRequest(
  item: DirectoryReviewItem,
  decision: ReviewDecision,
  choice: SetupChoice | undefined,
): ResolveDirectoryReviewItemForm {
  if (decision !== "approve") return { id: item.id, decision };
  if (item.kind === "source_missing") {
    return { id: item.id, decision, version: item.item.version };
  }
  return {
    id: item.id,
    decision,
    version: item.item.version,
    roles: choice
      ? [
          choice.kind === "reuse"
            ? { roleUrn: choice.roleUrn }
            : { createName: choice.name },
        ]
      : [],
  };
}

/** The confirmation toast for a decision that succeeded. */
export function reviewDecisionMessage(
  item: DirectoryReviewItem,
  decision: ReviewDecision,
): string {
  const name = item.item.sourceName;
  if (decision === "opt_out_key") {
    return `New ${item.item.source.attributeKey ?? "attribute"} values won't be raised for review any more.`;
  }
  if (item.kind === "source_missing") {
    return decision === "approve"
      ? `Removed the roles from ${name}.`
      : `Kept the roles on ${name}.`;
  }
  return decision === "approve" ? `Mapped ${name}.` : `Left ${name} unmapped.`;
}
