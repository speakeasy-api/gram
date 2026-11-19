import { defineFilters, type FilterValues } from "@/components/filters";
import {
  customDateRangeBounds,
  type DateRangeValue,
} from "@/components/filters/filter-schema";
import { DATE_RANGE_PARAMS } from "@/components/filters/useFilterState";
import { safeBase64Encode } from "@/components/observe/observeFilterUtils";
import type { Window } from "./api";
import { formatClockMinutes, formatDayMonth } from "./format";
import {
  ATTEMPT_OUTCOMES,
  STEP_GROUPS,
  type AttemptFilters,
  type Outcome,
  type StepGroup,
} from "./types";

export const TIME_RANGE_PRESETS = ["1h", "4h", "1d", "7d", "30d"] as const;
export type TimeRangePreset = (typeof TIME_RANGE_PRESETS)[number];

export const DEFAULT_LOG_RANGE: TimeRangePreset = "1d";
export const RETENTION_MS = 30 * 24 * 60 * 60 * 1000;

const PRESET_MS: Record<TimeRangePreset, number> = {
  "1h": 60 * 60 * 1000,
  "4h": 4 * 60 * 60 * 1000,
  "1d": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
  "30d": RETENTION_MS,
};

export const LOG_FILTERS = defineFilters([
  {
    id: "date",
    label: "Time range",
    kind: "daterange",
    pinned: true,
    defaultPreset: DEFAULT_LOG_RANGE,
    presets: [...TIME_RANGE_PRESETS],
    description: "Connection Logs cover the last 30 days.",
  },
  { id: "outcome", label: "Outcome", kind: "multiselect", pinned: true },
  {
    id: "server",
    label: "Server",
    kind: "select",
    pinned: true,
    chipPrefix: true,
  },
  { id: "service", label: "Service", kind: "select", chipPrefix: true },
  {
    id: "person",
    label: "Person",
    kind: "select",
    pinned: true,
    allLabel: "All people",
    description: "Attempts with no known person are listed by network.",
    chipPrefix: true,
  },
  { id: "app", label: "App", kind: "select", chipPrefix: true },
  {
    id: "appVersion",
    label: "App version",
    kind: "select",
    allLabel: "Any version",
    description:
      "Apps report a version only when they finish or renew a sign-in, or make a request.",
    chipPrefix: true,
  },
  { id: "step", label: "Step", kind: "multiselect", chipPrefix: true },
  {
    id: "message",
    label: "Message",
    kind: "multiselect",
    allLabel: "Anything",
    chipCount: true,
  },
  {
    id: "network",
    label: "Network",
    kind: "select",
    allLabel: "Any network",
    chipPrefix: true,
  },
]);

export type LogFilterValues = FilterValues<typeof LOG_FILTERS>;

/** On a server's own tab the server is fixed, so it has no chip of its own. */
export const LOG_FILTERS_WITHOUT_SERVER = LOG_FILTERS.filter(
  (dim) => dim.id !== "server",
);

/** The URL value for unlisted apps, whose family is "". */
export const UNLISTED_APP = "unlisted";

export const SEARCH_PARAM = "q";
export const ATTEMPT_PARAM = "attempt";
export const PROBLEM_PARAM = "problem";

const isPreset = (value: string | null): value is TimeRangePreset =>
  (TIME_RANGE_PRESETS as readonly (string | null)[]).includes(value);

/** The window a preset or a picked range covers, against `now`. */
export function resolveWindow(value: DateRangeValue, now: Date): Window {
  const bounds = customDateRangeBounds(value);
  if (bounds) {
    return { from: bounds.from.toISOString(), to: bounds.to.toISOString() };
  }
  return presetWindow(
    isPreset(value.preset) ? value.preset : DEFAULT_LOG_RANGE,
    now,
  );
}

export function presetWindow(preset: TimeRangePreset, now: Date): Window {
  return {
    from: new Date(now.getTime() - PRESET_MS[preset]).toISOString(),
    to: now.toISOString(),
  };
}

/** Whether any of the window falls before what the log still holds. */
export function startsBeforeRetention(window: Window, now: Date): boolean {
  return Date.parse(window.from) < now.getTime() - RETENTION_MS;
}

const nonEmpty = <T>(list: T[]): T[] | undefined =>
  list.length > 0 ? list : undefined;
const one = (value: string | null): string[] | undefined =>
  value === null ? undefined : [value];

/** The URL's filter values as the API reads them. */
export function toAttemptFilters(values: LogFilterValues): AttemptFilters {
  return {
    outcomes: nonEmpty(
      values.outcome.filter((o): o is Outcome =>
        (ATTEMPT_OUTCOMES as readonly string[]).includes(o),
      ),
    ),
    serverIds: one(values.server),
    services: one(values.service),
    people: one(values.person),
    apps: one(values.app === UNLISTED_APP ? "" : values.app),
    appVersions: one(values.appVersion),
    steps: nonEmpty(
      values.step.filter((s): s is StepGroup =>
        (STEP_GROUPS as readonly string[]).includes(s),
      ),
    ),
    reasons: nonEmpty(values.message),
    networks: one(values.network),
  };
}

type LinkFilters = AttemptFilters & {
  /** Open-ended start; the chip reads "Since …". */
  from?: string;
  range?: TimeRangePreset;
};

/** Query string that opens Connection Logs with these filters. */
export function logsSearch(filters: LinkFilters, now: Date): string {
  const params = new URLSearchParams();
  if (filters.from) {
    const from = new Date(filters.from);
    params.set("from", from.toISOString());
    params.set("to", now.toISOString());
    const sameDay = from.toDateString() === now.toDateString();
    params.set(
      "label",
      safeBase64Encode(
        `Since ${sameDay ? formatClockMinutes(filters.from) : formatDayMonth(filters.from)}`,
      ),
    );
  } else if (filters.range && filters.range !== DEFAULT_LOG_RANGE) {
    params.set("range", filters.range);
  }
  const set = (key: string, values: string[] | undefined, single = false) => {
    if (values && values.length > 0) {
      params.set(key, (single ? values.slice(0, 1) : values).join(","));
    }
  };
  set("outcome", filters.outcomes);
  set("server", filters.serverIds, true);
  set("service", filters.services, true);
  set("person", filters.people, true);
  set(
    "app",
    filters.apps?.map((a) => (a === "" ? UNLISTED_APP : a)),
    true,
  );
  set("appVersion", filters.appVersions, true);
  set("step", filters.steps);
  set("message", filters.reasons);
  set("network", filters.networks, true);
  const query = params.toString();
  return query ? `?${query}` : "";
}

/** Every parameter Connection Logs reads. */
export const LOG_PARAMS: readonly string[] = [
  ...DATE_RANGE_PARAMS,
  ...LOG_FILTERS.filter((d) => d.kind !== "daterange").map((d) => d.id),
  SEARCH_PARAM,
];
