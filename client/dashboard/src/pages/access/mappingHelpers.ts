import type { DirectoryAttributeTarget } from "@gram/client/models/components/directoryattributetarget.js";
import type { DirectoryGroupTarget } from "@gram/client/models/components/directorygrouptarget.js";
import type { DirectoryMapping } from "@gram/client/models/components/directorymapping.js";

export type MappingKind = "group" | "attribute";

export type MappingTargetOption = {
  principalUrn: string;
  kind: MappingKind;
  label: string;
  memberCount: number;
};

export function mappingLabel(
  mapping: Pick<
    DirectoryMapping,
    | "kind"
    | "groupName"
    | "groupId"
    | "attributeKey"
    | "attributeValue"
    | "principalUrn"
  >,
): string {
  if (mapping.kind === "group") {
    return mapping.groupName || mapping.groupId || mapping.principalUrn;
  }
  if (mapping.attributeKey && mapping.attributeValue) {
    return `${mapping.attributeKey}: ${mapping.attributeValue}`;
  }
  return mapping.principalUrn;
}

export function mappingKindLabel(kind: string): string {
  return kind === "group" ? "Group" : "Attribute";
}

export function unusedMappingTargets(
  groups: DirectoryGroupTarget[],
  attributes: DirectoryAttributeTarget[],
  mappedPrincipalUrns: Iterable<string>,
): MappingTargetOption[] {
  const mapped = new Set(mappedPrincipalUrns);
  const options: MappingTargetOption[] = [];

  for (const group of groups) {
    if (mapped.has(group.principalUrn)) continue;
    options.push({
      principalUrn: group.principalUrn,
      kind: "group",
      label: group.name,
      memberCount: group.memberCount,
    });
  }

  for (const attribute of attributes) {
    if (mapped.has(attribute.principalUrn)) continue;
    options.push({
      principalUrn: attribute.principalUrn,
      kind: "attribute",
      label: `${attribute.key}: ${attribute.value}`,
      memberCount: attribute.memberCount,
    });
  }

  return options.toSorted((a, b) => {
    if (a.kind !== b.kind) {
      return a.kind === "group" ? -1 : 1;
    }
    return a.label.localeCompare(b.label);
  });
}
