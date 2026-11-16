import { UpdateUserSessionIssuerFormClientIdMetadataAdmissionMode as WritableMode } from "@gram/client/models/components/updateusersessionissuerform.js";

// Shared by the MCP server's Sessions rows and an MCP Authentication
// Provider's settings form, so both describe a setting the same way.

export type DurationUnit = "hour" | "day" | "week";

export const DURATION_UNIT_HOURS: Record<DurationUnit, number> = {
  hour: 1,
  day: 24,
  week: 24 * 7,
};

export const DURATION_UNIT_OPTIONS: ReadonlyArray<{
  value: DurationUnit;
  label: string;
}> = [
  { value: "hour", label: "Hours" },
  { value: "day", label: "Days" },
  { value: "week", label: "Weeks" },
];

export function splitIntoUnit(hours: number): {
  number: number;
  unit: DurationUnit;
} {
  if (hours > 0 && hours % DURATION_UNIT_HOURS.week === 0) {
    return { number: hours / DURATION_UNIT_HOURS.week, unit: "week" };
  }
  if (hours > 0 && hours % DURATION_UNIT_HOURS.day === 0) {
    return { number: hours / DURATION_UNIT_HOURS.day, unit: "day" };
  }
  return { number: Math.max(0, hours), unit: "hour" };
}

// The three WRITABLE modes. "open" is what an issuer carries unless someone
// changes it, so it is what a newly created or never-configured issuer shows
// as selected. The read side of the API can also return "reporting", a
// legacy value that admits exactly what "open" admits. It is not writable
// and is deliberately not offered as a fourth option, so an issuer still
// storing it renders with nothing selected.
export const CLIENT_ACCESS_OPTIONS: {
  value: WritableMode;
  label: string;
  /** Shown only while this option is the selection. */
  explanation: string;
}[] = [
  {
    value: WritableMode.Presets,
    label: "Verified clients",
    // Deliberately not "the URLs you add below": the custom URL list only
    // renders in the modes that consult it, so "below" would point at
    // nothing for an issuer currently on Any or Off.
    explanation:
      "Clients Speakeasy has checked, plus any document URLs you allow yourself.",
  },
  {
    value: WritableMode.Open,
    label: "Any client",
    explanation:
      "Any client with a valid document, hosted anywhere on the internet. Nobody vets it first — each user decides at the consent screen.",
  },
  {
    value: WritableMode.Disabled,
    label: "Off",
    explanation:
      "No client connects this way. Speakeasy stops advertising it, so clients register themselves instead.",
  },
];
