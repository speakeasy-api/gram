import type { DeviceIntegrationProvider } from "@gram/client/models/components/deviceintegrationprovider.js";
import { MonitorSmartphone, ShieldCheck } from "lucide-react";

// Presentation extras the backend registry doesn't carry: an icon, a richer
// description, and optional setup guidance for the configure sheet. Provider
// entries are keyed by registry id; anything not listed gets a capability-
// derived default, so a new backend provider renders without frontend work
// and a provider PR can layer its own entry on top.
export type ProviderUI = {
  icon: React.ComponentType<{ className?: string }>;
  description: string;
  // Ordered setup steps rendered in the configure sheet, walking an admin
  // through minting least-privilege credentials in the vendor console.
  setupSteps?: string[];
};

const PROVIDER_UI: Record<string, ProviderUI> = {};

export function providerUI(provider: DeviceIntegrationProvider): ProviderUI {
  return PROVIDER_UI[provider.id] ?? defaultProviderUI(provider);
}

function defaultProviderUI(provider: DeviceIntegrationProvider): ProviderUI {
  const pulls = provider.capabilities.includes("inventory_source");
  const pushes = provider.capabilities.includes("evidence_sink");
  if (pulls && pushes) {
    return {
      icon: MonitorSmartphone,
      description:
        "Syncs the managed-device fleet and pushes agent coverage evidence.",
    };
  }
  if (pushes) {
    return {
      icon: ShieldCheck,
      description: "Pushes agent coverage evidence for compliance monitoring.",
    };
  }
  return {
    icon: MonitorSmartphone,
    description:
      "Syncs the managed-device fleet so agent coverage can be computed.",
  };
}
