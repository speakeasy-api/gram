import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";

/**
 * Who owns a provider or client, which decides who may edit it.
 *
 * - project: owned by one project.
 * - organization: shared across the organization's projects.
 * - platform: the curated catalog. Tenants inherit these read-only and may
 *   attach their own clients, but cannot edit, move, or delete them.
 */
export type ScopeTier = "project" | "organization" | "platform";

export function scopeTier(entity: {
  projectId?: string | null;
  organizationId?: string | null;
}): ScopeTier {
  if (entity.projectId) return "project";
  if (entity.organizationId) return "organization";
  return "platform";
}

/**
 * What a provider can do for us when we ask it to register a client.
 *
 * Read from the issuer record for a provider we already hold, and from a
 * metadata fetch for one we have only discovered. A provider that publishes
 * neither is not broken — it is a manual one, and plenty of real upstreams
 * (GitHub among them) are exactly that.
 */
export type ProviderCapabilities = {
  readonly cimd: boolean;
  readonly dcr: boolean;
  /** Where to send an operator who has to register a client by hand. */
  readonly registrationGuideUrl: string | null;
};

export function providerCapabilities(source: {
  clientIdMetadataDocumentSupported?: boolean;
  registrationEndpoint?: string | null;
  clientSetupDocumentationUrl?: string | null;
  serviceDocumentation?: string | null;
}): ProviderCapabilities {
  return {
    cimd: !!source.clientIdMetadataDocumentSupported,
    dcr: !!source.registrationEndpoint?.trim(),
    // An upstream's own "how to register an app" page is the best link we can
    // offer, and it is the one thing a manual-only provider does publish.
    registrationGuideUrl:
      source.clientSetupDocumentationUrl?.trim() ||
      source.serviceDocumentation?.trim() ||
      null,
  };
}

/** Can this provider register a client for us, or must the operator do it? */
export function supportsAutomaticRegistration(
  capabilities: ProviderCapabilities,
): boolean {
  return capabilities.cimd || capabilities.dcr;
}

/** Host and path, without the scheme — how an issuer URL reads in the UI. */
export function providerUrlLabel(issuerUrl: string): string {
  return issuerUrl.replace(/^https?:\/\//, "").replace(/\/$/, "");
}

/**
 * The name for a provider. Falls back to the issuer's host so a record with no
 * display name still reads as a place rather than a slug.
 */
export function providerDisplayName(issuer: {
  name?: string | null;
  issuer: string;
  slug?: string;
}): string {
  const named = issuer.name?.trim();
  if (named) return named;
  try {
    const host = new URL(issuer.issuer).hostname;
    if (host) return host;
  } catch {
    // Not a URL yet — an operator may still be typing it.
  }
  return issuer.slug ?? providerUrlLabel(issuer.issuer);
}

/**
 * The short label for a client. A CIMD client's client_id is the hosted
 * metadata-document URL, which is far too long to read inline, so those fall
 * back to the row id — which is also that URL's trailing path segment.
 * Surfaces that document the real client_id keep showing the full value.
 */
export function clientDisplayName(
  client: Pick<RemoteSessionClient, "id" | "clientId" | "clientIdMetadataUri">,
): string {
  if (client.clientIdMetadataUri) return client.id;
  return client.clientId;
}
