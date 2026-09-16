export type CreatedRange = { createdFrom?: string; createdTo?: string };
export type CreatedRangeKey = keyof CreatedRange;
type RawRange = { createdFrom?: unknown; createdTo?: unknown };

export const CREATED_PRESETS = [
  { value: "all", label: "All time" },
  { value: "today", label: "Today" },
  { value: "7", label: "Last 7 days" },
  { value: "14", label: "Last 14 days" },
  { value: "30", label: "Last 30 days" },
  { value: "custom", label: "Custom" },
] as const;
export type CreatedPreset = (typeof CREATED_PRESETS)[number]["value"];

export type RelativeCreatedPreset = Exclude<CreatedPreset, "all" | "custom">;
export type CreatedSelection = CreatedRange & {
  createdPreset?: RelativeCreatedPreset;
};

/** Metadata describes a saved snapshot, never a request to advance its bounds. */
export function selectedCreatedPreset(
  raw: RawRange & { createdPreset?: unknown },
  now = new Date(),
): CreatedPreset {
  const range = createdRange(raw);
  if (!range.createdFrom && !range.createdTo) return "all";
  const preset = raw.createdPreset;
  if (
    preset === "today" ||
    preset === "7" ||
    preset === "14" ||
    preset === "30"
  ) {
    if (recognizeCreatedPreset(range, now) === preset) return preset;
  }
  // Manual URLs and explicitly chosen Custom must not be inferred as relative.
  return "custom";
}

export function createdPresetMetadata(
  raw: RawRange & { createdPreset?: unknown },
  now = new Date(),
): RelativeCreatedPreset | undefined {
  const preset = selectedCreatedPreset(raw, now);
  return preset === "all" || preset === "custom" ? undefined : preset;
}

function dateBound(value: unknown): string | undefined {
  if (
    typeof value !== "string" ||
    !/^[0-9]{4}-[0-9]{2}-[0-9]{2}$/.test(value) ||
    value.startsWith("0000")
  )
    return undefined;
  const instant = new Date(`${value}T00:00:00Z`);
  return Number.isFinite(instant.getTime()) &&
    instant.toISOString().slice(0, 10) === value
    ? value
    : undefined;
}

export function createdRangeErrors(
  raw: RawRange,
): Partial<Record<CreatedRangeKey, string>> {
  const errors: Partial<Record<CreatedRangeKey, string>> = {};
  for (const key of ["createdFrom", "createdTo"] as const) {
    if (
      raw[key] !== undefined &&
      raw[key] !== "" &&
      dateBound(raw[key]) === undefined
    )
      errors[key] = "Enter a valid date as YYYY-MM-DD.";
  }
  const from = dateBound(raw.createdFrom);
  const to = dateBound(raw.createdTo);
  if (from && to && from > to)
    errors.createdTo = "To must be on or after From.";
  return errors;
}

/** Forgiving URL policy: invalid endpoints separately, reversed pairs together. */
export function createdRange(raw: RawRange): CreatedRange {
  const createdFrom = dateBound(raw.createdFrom);
  const createdTo = dateBound(raw.createdTo);
  return createdFrom && createdTo && createdFrom > createdTo
    ? { createdFrom: undefined, createdTo: undefined }
    : { createdFrom, createdTo };
}

/** Resolve relative choices at Apply, using inclusive UTC calendar days. */
export function createdPresetRange(
  preset: Exclude<CreatedPreset, "custom">,
  now = new Date(),
): CreatedRange {
  if (preset === "all") return { createdFrom: undefined, createdTo: undefined };
  const end = new Date(now);
  end.setUTCHours(0, 0, 0, 0);
  const start = new Date(end);
  start.setUTCDate(
    start.getUTCDate() - (preset === "today" ? 0 : Number(preset) - 1),
  );
  return {
    createdFrom: start.toISOString().slice(0, 10),
    createdTo: end.toISOString().slice(0, 10),
  };
}

export function recognizeCreatedPreset(
  range: CreatedRange,
  now = new Date(),
): CreatedPreset {
  if (!range.createdFrom && !range.createdTo) return "all";
  for (const preset of ["today", "7", "14", "30"] as const) {
    const candidate = createdPresetRange(preset, now);
    if (
      candidate.createdFrom === range.createdFrom &&
      candidate.createdTo === range.createdTo
    )
      return preset;
  }
  return "custom";
}

/** Absolute labels never become stale when midnight passes. */
export function createdRangeSummary(range: CreatedRange): string {
  if (range.createdFrom && range.createdTo)
    return `${range.createdFrom} ≤ date ≤ ${range.createdTo} (UTC)`;
  if (range.createdFrom) return `≥ ${range.createdFrom} (UTC)`;
  if (range.createdTo) return `≤ ${range.createdTo} (UTC)`;
  return "All time";
}
