import type { DeviceIntegrationProvider } from "@gram/client/models/components/deviceintegrationprovider.js";
import { Apple, Laptop, MonitorSmartphone, ShieldCheck } from "lucide-react";

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

const PROVIDER_UI: Record<string, ProviderUI> = {
  intune: {
    icon: Laptop,
    description:
      "Pulls the managed-device inventory from Microsoft Intune via Microsoft Graph so agent coverage can be computed across your fleet.",
    setupSteps: [
      "In Entra ID, create an app registration and grant it only the DeviceManagementManagedDevices.Read.All application permission, then grant admin consent.",
      "Create a client secret for the app and copy the directory (tenant) ID, application (client) ID, and the secret value.",
      "Enter all three, save, then test the connection.",
    ],
  },
  iru: {
    icon: Apple,
    description:
      "Pulls the device inventory from your Iru (formerly Kandji) tenant so agent coverage can be computed across your Apple fleet.",
    setupSteps: [
      "In the Iru console, go to Settings → Access → API Token and create a token with only the “Device list” permission enabled.",
      "Copy the token and the tenant's API URL shown on the same page (https://yourtenant.api.iru.com; legacy *.api.kandji.io URLs also work).",
      "Enter the API URL and token, save, then test the connection.",
    ],
  },
  jamf: {
    icon: Apple,
    description:
      "Pulls the computer inventory from your Jamf Pro tenant so agent coverage can be computed across your Apple fleet.",
    setupSteps: [
      "In Jamf Pro, go to Settings → System → API roles and clients and create an API role with only the “Read Computers” privilege.",
      "Create an API client bound to that role, enable it, and copy the client ID and the one-time client secret.",
      "Enter your tenant root URL (https://yourtenant.jamfcloud.com) and the client credentials, save, then test the connection.",
    ],
  },
};

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
