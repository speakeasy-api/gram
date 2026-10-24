import { Operator as Op } from "@gram/client/models/components/logfilter";
import type { QueryUserSessionFacetsRequest } from "@gram/client/models/operations/queryusersessionfacets.js";
import { OP_LABELS, type ActiveLogFilter } from "@/pages/logs/log-filter-types";
import type { FilterKeyOption } from "@/pages/logs/LogFilterBar";

export const SESSION_FILTER_PARAM = "sf";

export type SessionFacetField =
  | "identity"
  | "identity_type"
  | "mcp_server"
  | "client"
  | "client_registration";

export const SESSION_FILTER_FIELDS: (FilterKeyOption & {
  key: SessionFacetField | "last_used";
  label: string;
})[] = [
  {
    key: "identity",
    label: "Identity",
    description: "Person, agent, workload or API key",
  },
  {
    key: "identity_type",
    label: "Identity type",
    description: "user | agent | workload | apikey | anonymous",
  },
  {
    key: "mcp_server",
    label: "Sign-in server",
    description: "Server that signed the session in",
  },
  {
    key: "client",
    label: "Client",
    description: "Connecting client (Claude Desktop, Cursor…)",
  },
  {
    key: "client_registration",
    label: "Client registration",
    description: "cimd | dcr | assistant | none",
  },
  {
    key: "last_used",
    label: "Last used",
    description: "< 1h, > 7d, or never for no recorded use",
  },
];

/** How a last_used chip reads: NULL is unknown, not proof of no use. */
export function lastUsedLabel(value: string): string {
  return value.trim().toLowerCase() === "never" ? "No recorded use" : value;
}

export const SESSION_FILTER_SAMPLES = [
  {
    value: "client_registration = cimd",
    label: "Clients admitted by metadata document",
  },
  { value: "identity_type = agent", label: "Connections held by agents" },
  { value: "last_used > 7d", label: "Dormant connections worth revoking" },
  { value: "last_used never", label: "Connections with no recorded use" },
];

export const IDENTITY_TYPE_LABELS: Record<string, string> = {
  user: "Person",
  agent: "Agent",
  workload: "Workload",
  apikey: "API key",
  anonymous: "Anonymous",
};

export const CLIENT_REGISTRATION_LABELS: Record<string, string> = {
  cimd: "Metadata document (CIMD)",
  dcr: "Dynamic registration (DCR)",
  assistant: "Assistant workload",
  none: "No client",
};

export const CLIENT_VALUE_LABELS: Record<string, string> = {
  dashboard: "Dashboard",
  none: "No client",
};

/** A label from a fixed map, ignoring inherited keys such as `toString`. */
export function ownLabel(
  labels: Record<string, string>,
  value: string,
): string | undefined {
  return Object.hasOwn(labels, value) ? labels[value] : undefined;
}

export function sessionFilterOperators(key: string): Op[] {
  return key === "last_used" ? [Op.Eq] : [Op.Eq, Op.NotEq, Op.In];
}

/** Percent-encodes each `in` item so a comma inside a value cannot split it. */
export function encodeValues(values: string[]): string {
  return values.map(encodeURIComponent).join(",");
}

export function decodeValues(value: string): string[] {
  return value
    .split(",")
    .map((part) => {
      // Separator spaces are trimmed; encoded spaces inside a value survive.
      const raw = part.trim();
      try {
        return decodeURIComponent(raw);
      } catch {
        return raw;
      }
    })
    .filter((part) => part !== "");
}

const DURATION_SECONDS: Record<string, number> = { m: 60, h: 3600, d: 86400 };

/** The server's last-used limit: ten years. */
const MAX_LAST_USED_SECONDS = 10 * 365 * 24 * 60 * 60;

export const LAST_USED_OPTIONS = ["< 1h", "< 24h", "> 7d", "never"];

/** `last_used > 7d` becomes an `=` chip whose value holds the comparison. */
export function parseSessionFilterExpression(
  input: string,
): { key: string; op: Op; value: string } | null {
  const match = /^last_used\b\s*=?\s*(.+)$/i.exec(input.trim());
  if (!match) return null;
  return { key: "last_used", op: Op.Eq, value: match[1]!.trim() };
}

export function parseLastUsed(
  value: string,
): { within?: number; olderThan?: number; never?: true } | null {
  const trimmed = value.trim().toLowerCase();
  if (trimmed === "never") return { never: true };
  const match = /^([<>])\s*(\d+)\s*([mhd])$/.exec(trimmed);
  if (!match) return null;
  const seconds = Number(match[2]) * DURATION_SECONDS[match[3]!]!;
  if (seconds < 1 || !(seconds <= MAX_LAST_USED_SECONDS)) return null;
  return match[1] === "<" ? { within: seconds } : { olderThan: seconds };
}

type FieldQuery = { include: string; exclude: string };

const QUERY_KEYS: Record<SessionFacetField, FieldQuery> = {
  identity: { include: "identities", exclude: "excludeIdentities" },
  identity_type: { include: "identityTypes", exclude: "excludeIdentityTypes" },
  mcp_server: { include: "servers", exclude: "excludeServers" },
  client: { include: "clients", exclude: "excludeClients" },
  client_registration: {
    include: "clientRegistrations",
    exclude: "excludeClientRegistrations",
  },
};

export type SessionFilterQuery = Pick<
  QueryUserSessionFacetsRequest,
  | "identities"
  | "excludeIdentities"
  | "identityTypes"
  | "excludeIdentityTypes"
  | "servers"
  | "excludeServers"
  | "clients"
  | "excludeClients"
  | "clientRegistrations"
  | "excludeClientRegistrations"
  | "lastUsedWithin"
  | "lastUsedOlderThan"
  | "lastUsedNever"
>;

export function includedValues(
  filters: ActiveLogFilter[],
  field: SessionFacetField,
): string[] {
  const values = new Set<string>();
  for (const filter of filters) {
    if (filter.path !== field || filter.value === undefined) continue;
    if (filter.op === Op.Eq) values.add(filter.value);
    if (filter.op === Op.In) {
      for (const v of decodeValues(filter.value)) values.add(v);
    }
  }
  return [...values];
}

/** Values within one field combine with OR; fields combine with AND. */
export function filtersToQuery(filters: ActiveLogFilter[]): SessionFilterQuery {
  const query: Record<string, unknown> = {};
  for (const [field, keys] of Object.entries(QUERY_KEYS) as [
    SessionFacetField,
    FieldQuery,
  ][]) {
    const include = includedValues(filters, field);
    const exclude = filters
      .filter(
        (f) => f.path === field && f.op === Op.NotEq && f.value !== undefined,
      )
      .map((f) => f.value!);
    if (include.length > 0) query[keys.include] = include;
    if (exclude.length > 0) query[keys.exclude] = [...new Set(exclude)];
  }
  // Last-used chips AND: the tightest bound of each kind applies.
  for (const filter of filters) {
    if (filter.path !== "last_used" || filter.value === undefined) continue;
    const parsed = parseLastUsed(filter.value);
    if (parsed?.within) {
      query.lastUsedWithin = Math.min(
        parsed.within,
        (query.lastUsedWithin as number | undefined) ?? Infinity,
      );
    }
    if (parsed?.olderThan) {
      query.lastUsedOlderThan = Math.max(
        parsed.olderThan,
        (query.lastUsedOlderThan as number | undefined) ?? 0,
      );
    }
    if (parsed?.never) query.lastUsedNever = true;
  }
  return query as SessionFilterQuery;
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

const VALID_VALUE: Record<SessionFacetField, (value: string) => boolean> = {
  identity: (value) => value !== "" && !value.includes("\0"),
  identity_type: (value) => Object.hasOwn(IDENTITY_TYPE_LABELS, value),
  mcp_server: (value) => UUID.test(value),
  client: (value) =>
    Object.hasOwn(CLIENT_VALUE_LABELS, value) || UUID.test(value),
  client_registration: (value) =>
    Object.hasOwn(CLIENT_REGISTRATION_LABELS, value),
};

/** The server's cap on each include and exclude list. */
const MAX_FILTER_VALUES = 50;

/** Why a set of chips would exceed a list's cap, or null when none does. */
export function sessionFilterLimitProblem(
  filters: ActiveLogFilter[],
): string | null {
  const query = filtersToQuery(filters) as Record<string, unknown>;
  const full = Object.values(query).some(
    (list) => Array.isArray(list) && list.length > MAX_FILTER_VALUES,
  );
  return full
    ? `A field takes at most ${MAX_FILTER_VALUES} values to include and ${MAX_FILTER_VALUES} to exclude`
    : null;
}

/** Drops a field's include or exclude chips once their values exceed the cap. */
export function withinFilterLimits(
  filters: ActiveLogFilter[],
): ActiveLogFilter[] {
  return filters.filter((filter) => {
    if (!Object.hasOwn(QUERY_KEYS, filter.path)) return true;
    const field = filter.path as SessionFacetField;
    const values =
      filter.op === Op.NotEq
        ? new Set(
            filters
              .filter((f) => f.path === field && f.op === Op.NotEq)
              .map((f) => f.value),
          ).size
        : includedValues(filters, field).length;
    return values <= MAX_FILTER_VALUES;
  });
}

/** Why a chip would not narrow the list as shown, or null when it would. */
export function sessionFilterProblem(
  path: string,
  op: Op,
  value: string | undefined,
): string | null {
  const field = SESSION_FILTER_FIELDS.find((option) => option.key === path);
  if (!field) return `Unknown field ${path}`;
  if (!sessionFilterOperators(path).includes(op) || value === undefined) {
    return `${field.label} doesn't support that operator`;
  }
  if (path === "last_used") {
    return parseLastUsed(value)
      ? null
      : "Last used takes < 1h, > 7d (up to 3650d) or never";
  }
  const values = op === Op.In ? decodeValues(value) : [value];
  return values.length > 0 &&
    values.every(VALID_VALUE[path as SessionFacetField])
    ? null
    : `Not a valid ${field.label.toLowerCase()} value`;
}

export function formatSessionFilterValue(
  path: string,
  op: Op,
  value: string,
  label: (path: string, value: string) => string,
): string {
  return op === Op.In
    ? decodeValues(value)
        .map((v) => label(path, v))
        .join(", ")
    : label(path, value);
}

/** A filter as the user reads it; values that don't apply are shown raw. */
export function describeSessionFilter(
  filter: ActiveLogFilter,
  label: (path: string, value: string) => string,
): string {
  const field = SESSION_FILTER_FIELDS.find((o) => o.key === filter.path);
  const value = filter.value ?? "";
  const shown =
    sessionFilterProblem(filter.path, filter.op, filter.value) === null
      ? formatSessionFilterValue(filter.path, filter.op, value, label)
      : filter.op === Op.In
        ? decodeValues(value).join(", ")
        : value;
  return `${field?.label ?? filter.path} ${OP_LABELS[filter.op] ?? filter.op} ${shown}`;
}

export function withIncludedValues(
  filters: ActiveLogFilter[],
  field: SessionFacetField,
  values: string[],
): ActiveLogFilter[] {
  const rest = filters.filter(
    (f) => !(f.path === field && (f.op === Op.Eq || f.op === Op.In)),
  );
  if (values.length === 0) return rest;
  return [
    ...rest,
    values.length === 1
      ? { id: crypto.randomUUID(), path: field, op: Op.Eq, value: values[0] }
      : {
          id: crypto.randomUUID(),
          path: field,
          op: Op.In,
          value: encodeValues(values),
        },
  ];
}
