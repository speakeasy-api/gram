import {
  EMAProviderKind,
  UnavailableReason,
  type EMAProvider,
} from "@gram/client/models/components/emaprovider.js";
import { isKnownEnumValue } from "@/lib/enum";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { formatRelativeTime } from "@/lib/dates";

import {
  identityProvidersHref,
  oktaViewHref,
  singleSignOnHref,
  speakeasyProviderHref,
} from "../tabs";

/** Where an admin fixes a provider that can't vouch. */
type ProviderFix =
  | "speakeasy"
  | "okta"
  | "identity-providers"
  | "verify-domain";

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

const PLACEHOLDER_PROVIDER_NAME = "Your identity providers";

/** Who vouches when every identity provider is enabled for a service, said the same way everywhere. */
export const DEFAULT_ACCEPTS_RULE =
  "By default, all identity providers are enabled for every service. Select Manage to change that.";

/** What the default's Speakeasy entry covers. */
export const SPEAKEASY_DEFAULT_SCOPE = "For servers with Speakeasy sign-in";

/** What the default's external placeholder covers. */
export const PLACEHOLDER_SCOPE =
  "For servers with identity provider sign-in. Set this up in that identity provider’s Cross App Access.";

function providerKey(provider: EMAProvider): string {
  if (!isKnownEnumValue(EMAProviderKind, provider.kind))
    return `unknown-${provider.kind}-${provider.remoteSessionIssuerId ?? provider.internalSessionIssuerId ?? "default"}`;
  return provider.kind === "speakeasy"
    ? "speakeasy"
    : `external-${provider.remoteSessionIssuerId ?? "default"}`;
}

/** Unknown provider settings must not be interpreted or overwritten. */
export function supportsProvider(
  provider: Pick<EMAProvider, "kind" | "unavailableReason">,
): boolean {
  return (
    isKnownEnumValue(EMAProviderKind, provider.kind) &&
    (provider.unavailableReason === undefined ||
      isKnownEnumValue(UnavailableReason, provider.unavailableReason))
  );
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
    signInName,
  }: {
    issuer: RemoteSessionIssuer | undefined;
    speakeasyIssuerUrl: string | undefined;
    /** The Okta connection's issuer record, when the organization has one. */
    oktaIssuerId?: string;
    /** The identity provider a server's sign-in goes through, on a server page. */
    signInName?: string;
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
  if (!supportsProvider(provider)) {
    return {
      ...base,
      name:
        provider.kind === "speakeasy"
          ? "Speakeasy"
          : provider.kind === "external"
            ? issuerDisplayName(issuer)
            : "Unknown identity provider",
      usable: false,
      unavailable:
        "Unknown availability. Refresh the page to check for an update.",
    };
  }
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
          unavailable: "People don’t log in to this server with it",
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
  const name =
    provider.kind === "external" ? issuerDisplayName(issuer) : "Speakeasy";
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
        unavailable: "Not working right now",
        fix: "speakeasy",
      };
    case "not_trusted_by_endpoint":
      // The fix is the service's enabled providers or the sign-in, not the Speakeasy issuer.
      return {
        ...base,
        name,
        usable: false,
        unavailable: `Can’t vouch for people who log in with ${signInName ?? "an identity provider"}`,
      };
    case "verified_domain_required":
      return {
        ...base,
        name,
        usable: false,
        unavailable: "Needs a verified domain",
        fix: "verify-domain",
      };
    case "deleted":
    case "disabled":
    case "not_rolled_out":
    case "setup_required":
      return {
        ...base,
        name,
        usable: false,
        unavailable: "Not set up",
        fix: "speakeasy",
      };
  }
  return {
    ...base,
    name,
    usable: false,
    unavailable:
      "Unknown availability. Refresh the page to check for an update.",
  };
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
    case "verify-domain":
      return {
        href: singleSignOnHref(),
        label: "Verify a domain on the Single sign-on tab",
      };
  }
}

/** Whether this provider has worked at the service, in the admin's words. */
export function acceptanceLabel(lastAcceptedAt: Date | undefined): string {
  return lastAcceptedAt
    ? `Last worked ${formatRelativeTime(lastAcceptedAt)}`
    : "Hasn’t worked yet";
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

/** The step under an issuer URL that hasn't worked at the service yet. */
export function addIssuerStep(service: string): string {
  return `Add this issuer URL in ${service}’s admin settings, under Enterprise Managed Authorization.`;
}

/** The setup step for an external identity provider, done in its Cross App Access. */
export function crossAppAccessStep(service: string, provider: string): string {
  return `Set up ${service} in ${provider}’s Cross App Access`;
}

/** Why trust is the admin's job, said once on the service page. */
export function trustCaveat(service: string): string {
  return `Set this up once in ${service} for each identity provider. Speakeasy can’t do it for you, and not every service supports it.`;
}

/** A server's name, else its slug. */
export function serverDisplayName(server: {
  name: string;
  slug: string;
}): string {
  return server.name.trim() || server.slug || "Unnamed server";
}

/** The sign-in a server's people use, which decides who can vouch for them. */
export type ServerSignIn = {
  /** The identity provider the sign-in trusts; absent for Speakeasy's own sign-in. */
  signInIssuerId?: string;
  signInIssuerName?: string;
};

/** One provider enabled for a service, saved or drafted. Stored as an ordered list. */
export type AcceptedProvider = {
  kind: EMAProvider["kind"];
  remoteSessionIssuerId?: string;
};

/** The sign-in's identity provider by name, or Speakeasy's own sign-in. */
function signInProviderName(server: ServerSignIn): string {
  if (!server.signInIssuerId) return "Speakeasy";
  return server.signInIssuerName?.trim() || "its identity provider";
}

/**
 * Whether the enabled providers include the one that can vouch for people on
 * a server: Speakeasy for a server with Speakeasy sign-in, the server's own
 * identity provider for one with identity provider sign-in. Nobody else can.
 * With nothing saved (null) every provider is enabled, so every server is
 * covered.
 */
export function vouchedFor(
  accepted: AcceptedProvider[] | null,
  server: ServerSignIn,
): boolean {
  if (accepted === null) return true;
  return accepted.some((entry) =>
    server.signInIssuerId
      ? entry.kind === "external" &&
        entry.remoteSessionIssuerId === server.signInIssuerId
      : entry.kind === "speakeasy",
  );
}

/** The servers the enabled providers leave with nobody to vouch for their people. */
export function strandedServers<T extends ServerSignIn>(
  accepted: AcceptedProvider[] | null,
  servers: T[],
): T[] {
  return servers.filter((server) => !vouchedFor(accepted, server));
}

/** Why a stranded server needs attention, short enough for a table cell. */
export function strandedReason(server: ServerSignIn, service: string): string {
  const provider =
    server.signInIssuerId && !server.signInIssuerName?.trim()
      ? "That server’s identity provider"
      : signInProviderName(server);
  return `${provider} isn’t enabled for ${service}`;
}

/**
 * What the enabled providers do to the servers they strand, one line per
 * sign-in, e.g. "Linear (Okta team) would lose Enterprise Managed
 * Authorization, because it uses Okta sign-in." A draft says what would
 * happen; a saved choice says what is happening and how to fix it.
 */
export function strandedNotes(
  stranded: (ServerSignIn & { name: string })[],
  { draft = false }: { draft?: boolean } = {},
): string[] {
  const groups = new Map<string, (ServerSignIn & { name: string })[]>();
  for (const server of stranded) {
    const key = server.signInIssuerId ?? "speakeasy";
    const group = groups.get(key);
    if (group) group.push(server);
    else groups.set(key, [server]);
  }
  return [...groups.values()].flatMap((servers) => {
    const [first] = servers;
    if (!first) return [];
    const provider = signInProviderName(first);
    const names = joinNames(servers.map((server) => server.name));
    const one = servers.length === 1;
    const named = provider !== "its identity provider";
    const who = one ? "it" : "they";
    const uses = `${one ? "uses" : "use"} ${named ? provider : "identity provider"} sign-in`;
    const enable = named ? provider : "that identity provider";
    return [
      draft
        ? `${names} would lose Enterprise Managed Authorization, because ${who} ${uses}.`
        : `${names} can’t use Enterprise Managed Authorization, because ${who} ${uses}. Enable ${enable}, or go back to the default.`,
    ];
  });
}

export function joinNames(names: string[]): string {
  if (names.length <= 1) return names[0] ?? "";
  return `${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;
}
