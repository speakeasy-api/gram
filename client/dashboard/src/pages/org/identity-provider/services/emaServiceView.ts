import type { EMAProvider } from "@gram/client/models/components/emaprovider.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { formatRelativeTime } from "@/lib/dates";

/**
 * One identity provider as it appears wherever identity chaining is explained:
 * who it is, whether it can vouch right now, and whether the service has ever
 * accepted it. Shared by the server page and the organization's Services view
 * so both say the same thing.
 */
export type ProviderView = {
  key: string;
  kind: EMAProvider["kind"];
  name: string;
  /** The issuer URL the service has to trust. */
  issuerUrl?: string;
  /** Whether it can vouch right now. */
  usable: boolean;
  /** Why it can't, in the admin's words. Empty when it can. */
  unavailable: string;
  /** Where to fix it, when the fix is a Speakeasy setting. */
  fix?: "speakeasy" | "okta";
  lastAcceptedAt?: Date;
  /** True when this sign-in's endpoint doesn't use it: noise on a server page. */
  unusedBySignIn: boolean;
};

export function providerKey(provider: EMAProvider): string {
  return provider.kind === "speakeasy"
    ? "speakeasy"
    : `external-${provider.remoteSessionIssuerId ?? "trusted"}`;
}

export function issuerDisplayName(
  issuer: RemoteSessionIssuer | undefined,
): string {
  return issuer?.name?.trim() || "Identity provider";
}

export function providerView(
  provider: EMAProvider,
  {
    issuer,
    speakeasyIssuerUrl,
  }: {
    issuer: RemoteSessionIssuer | undefined;
    speakeasyIssuerUrl: string | undefined;
  },
): ProviderView {
  const base = {
    key: providerKey(provider),
    kind: provider.kind,
    lastAcceptedAt: provider.lastAcceptedAt ?? undefined,
    unusedBySignIn: provider.unavailableReason === "not_trusted_by_endpoint",
  };
  if (provider.kind === "external") {
    const name = issuerDisplayName(issuer);
    switch (provider.unavailableReason) {
      case undefined:
        return {
          ...base,
          name,
          issuerUrl: issuer?.issuer,
          usable: true,
          unavailable: "",
        };
      case "not_trusted_by_endpoint":
        return {
          ...base,
          name,
          usable: false,
          unavailable: "People don’t sign in to this server with it",
        };
      case "deleted":
      case "disabled":
      case "keyless":
      case "not_rolled_out":
      case "readiness_unknown":
      case "setup_required":
      case "sign_in_enforcement_required":
      case "signing_paused":
      case "signing_unavailable":
      case "verified_domain_required":
        return {
          ...base,
          name,
          usable: false,
          unavailable: "No longer available",
          fix: "okta",
        };
    }
  }
  const name = "Speakeasy";
  switch (provider.unavailableReason) {
    case undefined:
      return {
        ...base,
        name,
        issuerUrl: speakeasyIssuerUrl,
        usable: true,
        unavailable: "",
      };
    case "sign_in_enforcement_required":
      return {
        ...base,
        name,
        usable: false,
        unavailable: "Needs sign-in enforcement for the organization",
        fix: "speakeasy",
      };
    case "signing_paused":
    case "signing_unavailable":
    case "keyless":
    case "readiness_unknown":
      return {
        ...base,
        name,
        usable: false,
        unavailable: "Can’t sign right now",
        fix: "speakeasy",
      };
    case "deleted":
    case "disabled":
    case "not_rolled_out":
    case "not_trusted_by_endpoint":
    case "setup_required":
    case "verified_domain_required":
      return {
        ...base,
        name,
        usable: false,
        unavailable: "Not set up",
        fix: "speakeasy",
      };
  }
}

/** Whether the service has accepted this provider, in the admin's words. */
export function acceptanceLabel(lastAcceptedAt: Date | undefined): string {
  return lastAcceptedAt
    ? `Last worked ${formatRelativeTime(lastAcceptedAt)}`
    : "Not accepted yet";
}

/** The service's display name, else the host its authorization server runs on. */
export function serviceName(
  name: string | undefined,
  issuerUrl: string,
): string {
  if (name?.trim()) return name.trim();
  try {
    return new URL(issuerUrl).host;
  } catch {
    return issuerUrl;
  }
}

/** Where the admin pastes an issuer URL, said once, the same way everywhere. */
export function trustInstruction(service: string): string {
  return `In ${service}’s admin settings, find Enterprise Managed Authorization and add this issuer URL. It’s a one-time step Speakeasy can’t do for you, and not every service offers it.`;
}
