import type { DeviceIntegrationProvider } from "@gram/client/models/components/deviceintegrationprovider.js";

// The two directions a device integration runs in. Sources PULL the managed
// fleet from an MDM; sinks PUSH the org-wide coverage out to a compliance
// platform. They are opposite operations, so the UI treats them differently —
// different detail pages, different language (synced vs pushed), and a device
// inventory that belongs to sources, not sinks.
export type ProviderRole = "source" | "sink";

// A provider is a sink when it can push evidence, otherwise a source. Each
// provider declares exactly one of these capabilities today; the sink test
// wins if a future provider ever declared both, because the sink page is the
// one that must NOT render a per-provider device inventory.
export function providerRole(
  provider: DeviceIntegrationProvider,
): ProviderRole {
  return provider.capabilities.includes("evidence_sink") ? "sink" : "source";
}

export function isSink(provider: DeviceIntegrationProvider): boolean {
  return providerRole(provider) === "sink";
}

// Role-specific verbs, kept in one place so "synced" never leaks onto a sink
// or "pushed" onto a source.
export const ROLE_COPY: Record<
  ProviderRole,
  {
    lastRunHeader: string;
    syncNowTooltip: string;
    syncNowAria: (schedule: string) => string;
    scheduleBlurb: string;
  }
> = {
  source: {
    lastRunHeader: "Last synced",
    syncNowTooltip: "Sync now — pull inventory immediately.",
    syncNowAria: (schedule) => `Sync ${schedule} now`,
    scheduleBlurb:
      "Each schedule pulls this MDM's inventory on its own cadence and can be paused or run immediately.",
  },
  sink: {
    lastRunHeader: "Last pushed",
    syncNowTooltip: "Push now — publish evidence immediately.",
    syncNowAria: (schedule) => `Push ${schedule} now`,
    scheduleBlurb:
      "Each schedule pushes the org-wide coverage on its own cadence and can be paused or run immediately.",
  },
};
