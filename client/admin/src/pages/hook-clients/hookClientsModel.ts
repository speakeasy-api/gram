import { z } from "zod";

import { gramAdminFetch } from "@/lib/gramAdminApi";

const organizationSchema = z.object({
  id: z.string(),
  name: z.string().optional(),
  slug: z.string().optional(),
});

const sessionCellSchema = z.object({
  organization_id: z.string(),
  day: z.string(),
  client: z.string(),
  machines: z.number(),
  sessions: z.number(),
  machines_all: z.number(),
});

const inferenceCellSchema = z.object({
  organization_id: z.string(),
  day: z.string(),
  surface: z.string(),
  users: z.number(),
  events: z.number(),
  sessions: z.number(),
});

export const hookClientsSchema = z.object({
  window_days: z.number(),
  queried_at: z.string(),
  from_cache: z.boolean(),
  organizations: z.array(organizationSchema),
  sessions: z.array(sessionCellSchema),
  inference: z.array(inferenceCellSchema),
});

export type HookClients = z.infer<typeof hookClientsSchema>;
export type SessionCell = z.infer<typeof sessionCellSchema>;
export type InferenceCell = z.infer<typeof inferenceCellSchema>;

export async function fetchHookClients(refresh: boolean): Promise<HookClients> {
  const path = refresh
    ? "/admin/hookClients.get?refresh=true"
    : "/admin/hookClients.get";
  return hookClientsSchema.parse(
    await gramAdminFetch<unknown>(path, { cache: "no-store" }),
  );
}

export const hookClientsQuery = {
  queryKey: ["hook-clients"],
  // The server keeps the grids for an hour, so a reload is cheap. Refresh
  // asks the server for a fresh ClickHouse read instead.
  queryFn: (): Promise<HookClients> => fetchHookClients(false),
  staleTime: 5 * 60 * 1000,
  refetchOnWindowFocus: false,
};

// How old the grids are, for the page header.
export function cacheAgeLabel(queriedAt: string, now: number): string {
  const minutes = Math.max(
    0,
    Math.floor((now - new Date(queriedAt).getTime()) / 60000),
  );
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  return `${hours} h ${minutes % 60} min ago`;
}

export const CURL = "curl";
export const BINARY_UNVERSIONED = "binary";

// The hook client scale runs from navy (curl) through blue (old binaries) to
// red (newest binary), interpolated in OKLab so steps look evenly spaced.
const STOPS: [number, string][] = [
  [0.0, "#0f3282"],
  [0.142, "#1358bb"],
  [0.283, "#2588d4"],
  [0.406, "#51afdc"],
  [0.477, "#5ec3ce"],
  [0.68, "#e1ba58"],
  [0.764, "#f2981d"],
  [0.871, "#ee6a06"],
  [1.0, "#db261d"],
];

function toLinear(c: number): number {
  return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
}

function toGamma(c: number): number {
  if (c <= 0) return 0;
  if (c >= 1) return 1;
  return c <= 0.0031308 ? 12.92 * c : 1.055 * Math.pow(c, 1 / 2.4) - 0.055;
}

function hexToOklab(hex: string): [number, number, number] {
  const r = toLinear(parseInt(hex.slice(1, 3), 16) / 255);
  const g = toLinear(parseInt(hex.slice(3, 5), 16) / 255);
  const b = toLinear(parseInt(hex.slice(5, 7), 16) / 255);
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  return [
    0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s,
    1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s,
    0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s,
  ];
}

function oklabToHex([L, A, B]: [number, number, number]): string {
  const l = Math.pow(L + 0.3963377774 * A + 0.2158037573 * B, 3);
  const m = Math.pow(L - 0.1055613458 * A - 0.0638541728 * B, 3);
  const s = Math.pow(L - 0.0894841775 * A - 1.291485548 * B, 3);
  const rgb = [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ];
  return (
    "#" +
    rgb
      .map((v) =>
        Math.round(toGamma(v) * 255)
          .toString(16)
          .padStart(2, "0"),
      )
      .join("")
  );
}

const STOP_LABS = STOPS.map(([, hex]) => hexToOklab(hex));

export function versionColor(t: number): string {
  const clamped = t > 0 ? Math.min(t, 1) : 0;
  const upper = Math.max(
    1,
    STOPS.findIndex(([at]) => at >= clamped),
  );
  const [fromAt, , fromLab] = stopAt(upper - 1);
  const [toAt, , toLab] = stopAt(upper);
  const u = toAt === fromAt ? 0 : (clamped - fromAt) / (toAt - fromAt);
  return oklabToHex([
    fromLab[0] + (toLab[0] - fromLab[0]) * u,
    fromLab[1] + (toLab[1] - fromLab[1]) * u,
    fromLab[2] + (toLab[2] - fromLab[2]) * u,
  ]);
}

function stopAt(index: number): [number, string, [number, number, number]] {
  const [at, hex] = STOPS[index] ?? STOPS[STOPS.length - 1] ?? [1, "#db261d"];
  return [at, hex, STOP_LABS[index] ?? hexToOklab(hex)];
}

function versionParts(version: string): number[] {
  return version
    .replace(/^v/, "")
    .split(/[.-]/)
    .map((part) => Number.parseInt(part, 10))
    .map((part) => (Number.isNaN(part) ? 0 : part));
}

export function compareVersions(a: string, b: string): number {
  const left = versionParts(a);
  const right = versionParts(b);
  for (let i = 0; i < Math.max(left.length, right.length); i++) {
    const diff = (left[i] ?? 0) - (right[i] ?? 0);
    if (diff !== 0) return diff;
  }
  return a.localeCompare(b);
}

export type Series = { key: string; label: string; color: string };

// Curl first, then binaries that record no version, then versions oldest to
// newest, so the stacked bars read bottom-up from oldest to newest client.
export function clientSeries(cells: SessionCell[]): Series[] {
  const clients = new Set(cells.map((c) => c.client).filter(Boolean));
  const versions = [...clients]
    .filter((c) => c !== CURL && c !== BINARY_UNVERSIONED)
    .sort(compareVersions);
  const series: Series[] = [];
  if (clients.has(CURL)) {
    series.push({ key: CURL, label: "curl", color: versionColor(0) });
  }
  if (clients.has(BINARY_UNVERSIONED)) {
    series.push({
      key: BINARY_UNVERSIONED,
      label: "binary, version not recorded",
      color: "#9198a1",
    });
  }
  versions.forEach((version, index) => {
    const t =
      versions.length === 1 ? 1 : 0.15 + (0.85 * index) / (versions.length - 1);
    series.push({ key: version, label: version, color: versionColor(t) });
  });
  return series;
}

const SURFACE_COLORS = [
  "#2a78d6",
  "#e07a1f",
  "#2f9e6e",
  "#c2459b",
  "#7a5af0",
  "#b5952f",
  "#5a6b7d",
];

export function surfaceSeries(cells: InferenceCell[]): Series[] {
  const totals = new Map<string, number>();
  for (const cell of cells) {
    if (!cell.surface) continue;
    totals.set(cell.surface, (totals.get(cell.surface) ?? 0) + cell.events);
  }
  return [...totals.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([surface], index) => ({
      key: surface,
      label: surface,
      color: SURFACE_COLORS[index % SURFACE_COLORS.length] ?? "#5a6b7d",
    }));
}

// The window's UTC days, oldest first, ending on the day the grid was read.
export function windowDays(queriedAt: string, days: number): string[] {
  const end = new Date(queriedAt);
  const out: string[] = [];
  for (let offset = days - 1; offset >= 0; offset--) {
    const day = new Date(
      Date.UTC(
        end.getUTCFullYear(),
        end.getUTCMonth(),
        end.getUTCDate() - offset,
      ),
    );
    out.push(day.toISOString().slice(0, 10));
  }
  return out;
}

export type DayCell = {
  // Per-series value the bar is split by.
  parts: Map<string, number>;
  // The number printed under the bar.
  label: number;
  // What "Sessions" and "Machines" bar heights scale by.
  sessions: number;
  machines: number;
  // Hooks arrived only from sessions started on an earlier day.
  stale: boolean;
};

export type OrgRow = {
  id: string;
  name: string;
  detail: string;
  total: number;
  days: Map<string, DayCell>;
};

type OrganizationNames = HookClients["organizations"];

function orgLabel(
  organizations: OrganizationNames,
  id: string,
): {
  name: string;
  detail: string;
} {
  const organization = organizations.find((o) => o.id === id);
  if (organization?.name) {
    return { name: organization.name, detail: organization.slug ?? id };
  }
  return { name: id.slice(0, 8), detail: "organization not found" };
}

export function sessionRows(data: HookClients): OrgRow[] {
  const rows = new Map<string, OrgRow>();
  const row = (id: string): OrgRow => {
    let found = rows.get(id);
    if (found) return found;
    found = {
      id,
      ...orgLabel(data.organizations, id),
      total: 0,
      days: new Map(),
    };
    rows.set(id, found);
    return found;
  };
  for (const cell of data.sessions) {
    if (cell.client) continue;
    const org = row(cell.organization_id);
    org.total += cell.sessions;
    org.days.set(cell.day, {
      parts: new Map(),
      label: cell.machines,
      sessions: cell.sessions,
      machines: cell.machines,
      stale: cell.machines === 0 && cell.machines_all > 0,
    });
  }
  for (const cell of data.sessions) {
    if (!cell.client) continue;
    const day = rows.get(cell.organization_id)?.days.get(cell.day);
    if (!day) continue;
    day.parts.set(cell.client, cell.machines);
  }
  return [...rows.values()].sort((a, b) => b.total - a.total);
}

export function inferenceRows(data: HookClients): OrgRow[] {
  const rows = new Map<string, OrgRow>();
  for (const cell of data.inference) {
    if (cell.surface) continue;
    let org = rows.get(cell.organization_id);
    if (!org) {
      org = {
        id: cell.organization_id,
        ...orgLabel(data.organizations, cell.organization_id),
        total: 0,
        days: new Map(),
      };
      rows.set(cell.organization_id, org);
    }
    org.total += cell.events;
    org.days.set(cell.day, {
      parts: new Map(),
      label: cell.users,
      sessions: cell.sessions,
      machines: cell.users,
      stale: false,
    });
  }
  for (const cell of data.inference) {
    if (!cell.surface) continue;
    const day = rows.get(cell.organization_id)?.days.get(cell.day);
    if (!day) continue;
    day.parts.set(cell.surface, cell.events);
  }
  return [...rows.values()].sort((a, b) => b.total - a.total);
}

export type LatestShares = {
  day: string;
  latestVersion: string | undefined;
  latestShare: number | undefined;
  curlShare: number | undefined;
  curlShareAll: number | undefined;
};

function share(part: number, whole: number): number | undefined {
  return whole > 0 ? part / whole : undefined;
}

// For an organization's latest day: among machines that started a session,
// the share on the newest binary version and on curl; and the curl share
// counting machines from every session.
export function latestShares(
  data: HookClients,
  organizationID: string,
  series: Series[],
): LatestShares | undefined {
  const cells = data.sessions.filter(
    (c) => c.organization_id === organizationID,
  );
  const totals = cells.filter((c) => !c.client);
  if (totals.length === 0) return undefined;
  const day = totals.map((c) => c.day).sort()[totals.length - 1];
  const total = totals.find((c) => c.day === day);
  if (!total) return undefined;
  const onDay = cells.filter((c) => c.day === day && c.client);
  const versions = series
    .map((s) => s.key)
    .filter((k) => k !== CURL && k !== BINARY_UNVERSIONED);
  const latestVersion = versions[versions.length - 1];
  const latest = onDay.find((c) => c.client === latestVersion);
  const curl = onDay.find((c) => c.client === CURL);
  return {
    day: total.day,
    latestVersion,
    latestShare: latestVersion
      ? share(latest?.machines ?? 0, total.machines)
      : undefined,
    curlShare: share(curl?.machines ?? 0, total.machines),
    curlShareAll: share(curl?.machines_all ?? 0, total.machines_all),
  };
}
