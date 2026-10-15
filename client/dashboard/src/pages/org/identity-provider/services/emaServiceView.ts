import type { EMAProvider } from "@gram/client/models/components/emaprovider.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { formatRelativeTime } from "@/lib/dates";

import {
  identityProvidersHref,
  oktaViewHref,
  speakeasyProviderHref,
} from "../tabs";

/** Where an admin fixes a provider that can't vouch. */
export type ProviderFix = "speakeasy" | "okta" | "identity-providers";

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
  /** The external provider's issuer record; absent for Speakeasy and the default placeholder. */
  remoteSessionIssuerId?: string;
  /** The organization-wide default's external entry: each server's own identity provider. */
  placeholder: boolean;
  /** The issuer URL the service has to trust. */
  issuerUrl?: string;
  /** Whether it can vouch right now. */
  usable: boolean;
  /** Why it can't, in the admin's words. Empty when it can. */
  unavailable: string;
  unavailableReason?: EMAProvider["unavailableReason"];
  /** Where to fix it. */
  fix?: ProviderFix;
  /** When the service last accepted this provider anywhere in the organization. */
  lastAcceptedAt?: Date;
  /** True when this sign-in's endpoint doesn't use it: noise on a server page. */
  unusedBySignIn: boolean;
};

const PLACEHOLDER_PROVIDER_NAME = "Each server’s own identity provider";

/** The rule that applies when no order is saved, said the same way everywhere. */
export const DEFAULT_ORDER_RULE =
  "With no saved order, a server whose sign-in uses an identity provider relies on that provider alone. A server whose sign-in has no identity provider relies on Speakeasy.";

/** What the default's Speakeasy entry covers. */
export const SPEAKEASY_DEFAULT_SCOPE =
  "For servers whose sign-in has no identity provider";

/** What the default's external placeholder covers. */
export const PLACEHOLDER_SCOPE =
  "For servers whose sign-in uses an identity provider. Its issuer URL is on that server’s Enterprise Managed Authorization settings.";

function providerKey(provider: EMAProvider): string {
  return provider.kind === "speakeasy"
    ? "speakeasy"
    : `external-${provider.remoteSessionIssuerId ?? "default"}`;
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
    oktaIssuerId,
  }: {
    issuer: RemoteSessionIssuer | undefined;
    speakeasyIssuerUrl: string | undefined;
    /** The Okta connection's issuer record, when the organization has one. */
    oktaIssuerId?: string;
  },
): ProviderView {
  const base = {
    key: providerKey(provider),
    kind: provider.kind,
    remoteSessionIssuerId: provider.remoteSessionIssuerId,
    placeholder: false,
    unavailableReason: provider.unavailableReason,
    lastAcceptedAt: provider.lastAcceptedAt ?? undefined,
    unusedBySignIn: provider.unavailableReason === "not_trusted_by_endpoint",
  };
  if (provider.kind === "external") {
    if (!provider.remoteSessionIssuerId) {
      return {
        ...base,
        placeholder: true,
        name: PLACEHOLDER_PROVIDER_NAME,
        usable: true,
        unavailable: "",
      };
    }
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
          fix:
            oktaIssuerId && provider.remoteSessionIssuerId === oktaIssuerId
              ? "okta"
              : "identity-providers",
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

/** The link that fixes a provider, as a search string on the Identity page. */
export function providerFixLink(fix: ProviderFix): {
  href: string;
  label: string;
} {
  switch (fix) {
    case "speakeasy":
      return { href: speakeasyProviderHref(), label: "Fix in Speakeasy" };
    case "okta":
      return { href: oktaViewHref("setup"), label: "Fix in Okta" };
    case "identity-providers":
      return { href: identityProvidersHref(), label: "Identity providers" };
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

/**
 * The external providers an order lists before Speakeasy. Runtime moves on
 * after any failure, so Speakeasy also vouches for people they refuse.
 */
export function providersBeforeSpeakeasy(
  order: { kind: EMAProvider["kind"]; name: string }[],
): string[] {
  const at = order.findIndex((entry) => entry.kind === "speakeasy");
  if (at <= 0) return [];
  return order
    .slice(0, at)
    .filter((entry) => entry.kind === "external")
    .map((entry) => entry.name);
}

/** What an order that lists Speakeasy after identity providers does, said plainly. */
export function speakeasyFallbackNote(names: string[]): string {
  const who =
    names.length <= 1
      ? (names[0] ?? "the identity provider before it")
      : `${names.slice(0, -1).join(", ")} or ${names.at(-1)}`;
  return `If ${who} can’t or won’t vouch for someone, Speakeasy is tried next.`;
}
