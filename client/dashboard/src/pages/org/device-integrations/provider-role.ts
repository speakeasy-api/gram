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

// Providers that are registered on the backend but not ready for general use.
// Hidden from the UI entirely — the list, the pipeline counts, the source
// breakdown, and direct-URL access — until they are fully supported. This is a
// deliberately temporary frontend gate: drop the id here to reveal it, no
// backend change needed.
//   - intune: inventory source not yet fully supported.
//   - vanta: evidence push is blocked on Vanta — custom resources aren't
//     supported for partner-built integrations, and they'd require each
//     customer to hand-author the compliance test. Hidden until Vanta offers a
//     supported path (e.g. a partner-writable device resource type).
const HIDDEN_PROVIDER_IDS = new Set(["intune", "vanta"]);

export function isProviderVisible(
  provider: DeviceIntegrationProvider,
): boolean {
  return !HIDDEN_PROVIDER_IDS.has(provider.id);
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
    retryStartedToast: string;
  }
> = {
  source: {
    lastRunHeader: "Last synced",
    syncNowTooltip: "Sync now — pull inventory immediately.",
    syncNowAria: (schedule) => `Sync ${schedule} now`,
    scheduleBlurb:
      "Each schedule pulls this MDM's inventory on its own cadence and can be paused or run immediately.",
    retryStartedToast: "Sync started. Fresh inventory should appear shortly.",
  },
  sink: {
    lastRunHeader: "Last pushed",
    syncNowTooltip: "Push now — publish evidence immediately.",
    syncNowAria: (schedule) => `Push ${schedule} now`,
    scheduleBlurb:
      "Each schedule pushes the org-wide coverage on its own cadence and can be paused or run immediately.",
    retryStartedToast: "Push started. Evidence should update shortly.",
  },
};
