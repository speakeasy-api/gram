import type { SetDirectoryRoleMappingForm } from "@gram/client/models/components/setdirectoryrolemappingform.js";

/**
 * The round trip from a directory role mapping to the role editor and back.
 * The mapping panel sends the source it was picking a role for; the editor
 * sends it back with the role it created, and the panel then saves the
 * mapping. Everything travels in the URL, so a reload mid-way keeps it.
 */
export const DIRECTORY_MAPPING_FLOW = "directory-mapping";

const MAP_ROLE = "mapRole";
const GROUP = "groupId";
const ATTRIBUTE_KEY = "attributeKey";
const ATTRIBUTE_VALUE = "attributeValue";
const SUGGESTED_NAME = "name";

type MappingSource = Omit<SetDirectoryRoleMappingForm, "roleUrn">;

function writeSource(params: URLSearchParams, source: MappingSource): void {
  if (source.sourceKind === "group" && source.directoryGroupId) {
    params.set(GROUP, source.directoryGroupId);
    return;
  }
  if (source.attributeKey && source.attributeValue) {
    params.set(ATTRIBUTE_KEY, source.attributeKey);
    params.set(ATTRIBUTE_VALUE, source.attributeValue);
  }
}

function readSource(params: URLSearchParams): MappingSource | undefined {
  const groupId = params.get(GROUP);
  if (groupId) return { sourceKind: "group", directoryGroupId: groupId };
  const key = params.get(ATTRIBUTE_KEY);
  const value = params.get(ATTRIBUTE_VALUE);
  if (key && value) {
    return {
      sourceKind: "attribute",
      attributeKey: key,
      attributeValue: value,
    };
  }
  return undefined;
}

/**
 * Query string that opens the role editor for a mapping source, with the
 * name the new role should start with (the group name or attribute value).
 */
export function createRoleForMappingParams(
  source: MappingSource,
  suggestedName: string,
): URLSearchParams {
  const params = new URLSearchParams({ from: DIRECTORY_MAPPING_FLOW });
  writeSource(params, source);
  if (suggestedName) params.set(SUGGESTED_NAME, suggestedName);
  return params;
}

/** The name the role editor should start with, if the flow suggested one. */
export function suggestedRoleName(params: URLSearchParams): string {
  return params.get(SUGGESTED_NAME) ?? "";
}

/** Query string that returns to the mapping panel with a created role. */
export function directoryMappingReturnParams(
  editorParams: URLSearchParams,
  roleUrn: string,
): URLSearchParams {
  const params = new URLSearchParams({ [MAP_ROLE]: roleUrn });
  const source = readSource(editorParams);
  if (source) writeSource(params, source);
  return params;
}

/** The mapping the panel should save on return, if the URL carries one. */
export function pendingMappingFromParams(
  params: URLSearchParams,
): SetDirectoryRoleMappingForm | undefined {
  const roleUrn = params.get(MAP_ROLE);
  const source = readSource(params);
  if (!roleUrn || !source) return undefined;
  return { ...source, roleUrn };
}

/** Removes the round-trip parameters once the mapping is saved. */
export function clearPendingMappingParams(
  params: URLSearchParams,
): URLSearchParams {
  const next = new URLSearchParams(params);
  for (const key of [MAP_ROLE, GROUP, ATTRIBUTE_KEY, ATTRIBUTE_VALUE]) {
    next.delete(key);
  }
  return next;
}
