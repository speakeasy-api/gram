import type { Role } from "@gram/client/models/components/role.js";
import type { SetDirectoryRoleMappingForm } from "@gram/client/models/components/setdirectoryrolemappingform.js";

/**
 * The round trip from a directory role mapping to the role editor and back.
 *
 * The mapping panel stores the source it was picking a role for in
 * sessionStorage under a random key, and only that key travels in the URL.
 * The editor records the role it created on the same entry, and the panel
 * then saves the mapping and deletes the entry. Keeping the data out of the
 * URL means a crafted link cannot trigger a mapping (it has no entry to point
 * at), and directory attribute values never reach history, logs or referrers.
 */
export const DIRECTORY_MAPPING_FLOW = "directory-mapping";

const FLOW_PARAM = "mapping";
const STORAGE_PREFIX = "gram.directoryRoleMappingFlow.";

type MappingSource = Omit<SetDirectoryRoleMappingForm, "roleUrn">;

type FlowRecord = {
  source: MappingSource;
  suggestedName: string;
  roleUrn?: string;
};

function readRecord(key: string): FlowRecord | undefined {
  try {
    const raw = window.sessionStorage.getItem(STORAGE_PREFIX + key);
    return raw ? (JSON.parse(raw) as FlowRecord) : undefined;
  } catch {
    return undefined;
  }
}

function writeRecord(key: string, record: FlowRecord): void {
  try {
    window.sessionStorage.setItem(STORAGE_PREFIX + key, JSON.stringify(record));
  } catch {
    // Storage can be unavailable; the flow then just skips the auto-map.
  }
}

/**
 * Starts the round trip and returns the query string that opens the role
 * editor for it. The new role starts with `suggestedName`.
 */
export function startCreateRoleFlow(
  source: MappingSource,
  suggestedName: string,
): URLSearchParams {
  const key = crypto.randomUUID();
  writeRecord(key, { source, suggestedName });
  return new URLSearchParams({
    from: DIRECTORY_MAPPING_FLOW,
    [FLOW_PARAM]: key,
  });
}

/**
 * Whether the role editor was opened by a round trip this tab started. A
 * link without a stored entry is treated as a plain role editor visit.
 */
export function isCreateRoleFlow(params: URLSearchParams): boolean {
  const key = params.get(FLOW_PARAM);
  return (
    params.get("from") === DIRECTORY_MAPPING_FLOW &&
    key !== null &&
    readRecord(key) !== undefined
  );
}

/** The name the role editor should start with, if the flow suggested one. */
export function suggestedRoleName(params: URLSearchParams): string {
  const key = params.get(FLOW_PARAM);
  return key ? (readRecord(key)?.suggestedName ?? "") : "";
}

/**
 * Records the role the editor created and returns the query string that
 * takes the panel back to save the mapping.
 */
export function completeCreateRoleFlow(
  editorParams: URLSearchParams,
  roleUrn: string,
): URLSearchParams {
  const key = editorParams.get(FLOW_PARAM);
  const record = key ? readRecord(key) : undefined;
  if (!key || !record) return new URLSearchParams();
  writeRecord(key, { ...record, roleUrn });
  return new URLSearchParams({ [FLOW_PARAM]: key });
}

/** The mapping to save on return, once the editor recorded a created role. */
export function pendingMappingFromParams(
  params: URLSearchParams,
): { key: string; form: SetDirectoryRoleMappingForm } | undefined {
  const key = params.get(FLOW_PARAM);
  const record = key ? readRecord(key) : undefined;
  if (!key || !record?.roleUrn) return undefined;
  return { key, form: { ...record.source, roleUrn: record.roleUrn } };
}

/**
 * Ends the round trip once the mapping is saved: drops the stored entry and
 * returns the query string without the flow key. A failed save keeps the
 * entry, so reloading the page retries it.
 */
export function finishCreateRoleFlow(
  params: URLSearchParams,
  key: string,
): URLSearchParams {
  try {
    window.sessionStorage.removeItem(STORAGE_PREFIX + key);
  } catch {
    // Nothing to clean up if storage is unavailable.
  }
  const next = new URLSearchParams(params);
  next.delete(FLOW_PARAM);
  return next;
}

/**
 * The first free role name starting from `base`: `base` itself, then
 * "base 2", "base 3" and so on. Role names are unique regardless of case.
 */
export function availableRoleName(
  base: string,
  roles: Pick<Role, "name">[],
): string {
  if (base === "") return "";
  const taken = new Set(roles.map((role) => role.name.toLowerCase()));
  if (!taken.has(base.toLowerCase())) return base;
  for (let n = 2; ; n++) {
    const candidate = `${base} ${n}`;
    if (!taken.has(candidate.toLowerCase())) return candidate;
  }
}
