import { discoveredFromDraft } from "@/pages/mcp/x/tabs/settings/sections/authentication/issuerFormUtils";
import { buildCreateIssuerForm } from "@/pages/remote-identity-providers/issuerSettingsForm";
import type { GramCore } from "@gram/client/core.js";
import { organizationRemoteSessionIssuersCreate } from "@gram/client/funcs/organizationRemoteSessionIssuersCreate.js";
import { organizationRemoteSessionIssuersFetchMetadata } from "@gram/client/funcs/organizationRemoteSessionIssuersFetchMetadata.js";
import { organizationRemoteSessionIssuersList } from "@gram/client/funcs/organizationRemoteSessionIssuersList.js";
import { organizationUserSessionIssuersUpdate } from "@gram/client/funcs/organizationUserSessionIssuersUpdate.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import type { RemoteSessionIssuerDraft } from "@gram/client/models/components/remotesessionissuerdraft.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { unwrapAsync } from "@gram/client/types/fp.js";

/** An identity provider found at the address the admin gave. */
export type FoundProvider = {
  /** The issuer its tokens name, as its metadata states it. */
  issuer: string;
  draft: RemoteSessionIssuerDraft;
};

/** Why an identity provider cannot be trusted for Enterprise Managed Auth. */
export class ProviderLookupError extends Error {}

/**
 * Reads an identity provider's published metadata. EMA checks the identity
 * token it vouches for with its signing keys, so a provider that publishes
 * none cannot be used.
 */
export async function lookUpProvider(
  core: GramCore,
  address: string,
): Promise<FoundProvider> {
  const issuer = normalizeIssuer(address);
  if (!issuer) {
    throw new ProviderLookupError(
      "Enter the provider's address, starting with https://.",
    );
  }
  let draft: RemoteSessionIssuerDraft;
  try {
    draft = await unwrapAsync(
      organizationRemoteSessionIssuersFetchMetadata(core, {
        fetchIssuerMetadataRequestBody: { issuer },
      }),
    );
  } catch (error) {
    // The server answers 400 when the address itself is the problem: nothing
    // there, or no metadata it can read. Anything else is on Speakeasy's side
    // or the person's access, and is reported as it is.
    if (error instanceof GramError && error.statusCode === 400) {
      throw new ProviderLookupError(
        "No identity provider answered at this address. Check it and try again.",
      );
    }
    throw error;
  }
  // The metadata must describe the provider that was asked for: one naming
  // another issuer could have EMA trust tokens nobody here chose to trust.
  if (draft.issuer && !sameIssuer(draft.issuer, issuer)) {
    throw new ProviderLookupError(
      `This address publishes metadata for ${draft.issuer}. Enter that address instead, or check with whoever runs the provider.`,
    );
  }
  if (!draft.jwksUri) {
    throw new ProviderLookupError(
      "This provider doesn't publish signing keys, so Speakeasy can't check the identity it vouches for.",
    );
  }
  if (!isHttps(draft.jwksUri)) {
    throw new ProviderLookupError(
      "This provider publishes its signing keys over an insecure connection, so Speakeasy can't rely on them.",
    );
  }
  return { issuer, draft };
}

function isHttps(address: string): boolean {
  try {
    return new URL(address).protocol === "https:";
  } catch {
    return false;
  }
}

/** "https://login.example.com/" and "login.example.com" name one issuer. */
export function normalizeIssuer(address: string): string | undefined {
  const trimmed = address.trim();
  if (trimmed === "") return undefined;
  try {
    const url = new URL(
      /^[a-z]+:\/\//i.test(trimmed) ? trimmed : `https://${trimmed}`,
    );
    if (url.protocol !== "https:") return undefined;
    return url.toString().replace(/\/+$/, "");
  } catch {
    return undefined;
  }
}

/** A slug for a new organization identity provider, free among `taken`. */
export function providerSlug(
  issuer: string,
  taken: ReadonlySet<string>,
): string {
  const host = new URL(issuer).hostname;
  const base =
    host
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "") || "identity-provider";
  let slug = base;
  for (let n = 2; taken.has(slug); n++) slug = `${base}-${n}`;
  return slug;
}

async function organizationProviders(
  core: GramCore,
): Promise<RemoteSessionIssuer[]> {
  const providers: RemoteSessionIssuer[] = [];
  let cursor: string | undefined;
  do {
    const page = await unwrapAsync(
      organizationRemoteSessionIssuersList(core, {
        tier: "organization",
        cursor,
        limit: 100,
      }),
    );
    providers.push(...page.result.items.map((item) => item.issuer));
    cursor = page.result.nextCursor;
  } while (cursor);
  return providers;
}

/**
 * Trusts `found` for the MCP Authentication Provider: reuses the
 * organization's record of that identity provider, or adds one, then points
 * the provider at it. Browser sign-in is left as it is.
 */
export async function turnOnEma(
  core: GramCore,
  provider: UserSessionIssuer,
  found: FoundProvider,
): Promise<UserSessionIssuer> {
  const existing = await organizationProviders(core);
  let trusted = existing.find(
    (item) => item.projectId === "" && sameIssuer(item.issuer, found.issuer),
  );
  if (!trusted) {
    const { draft } = found;
    trusted = await unwrapAsync(
      organizationRemoteSessionIssuersCreate(core, {
        createIssuerRequestBody: buildCreateIssuerForm({
          name: "",
          logoAssetId: "",
          clientSetupDocumentationUrl: "",
          slug: providerSlug(
            found.issuer,
            new Set(existing.map((i) => i.slug)),
          ),
          issuerUrl: found.issuer,
          authorizationEndpoint: draft.authorizationEndpoint ?? "",
          tokenEndpoint: draft.tokenEndpoint ?? "",
          registrationEndpoint: draft.registrationEndpoint ?? "",
          jwksUri: draft.jwksUri ?? "",
          discoveredSnapshot: discoveredFromDraft(found.issuer, draft),
        }),
      }),
    );
  }
  return unwrapAsync(
    organizationUserSessionIssuersUpdate(core, {
      updateOrganizationUserSessionIssuerForm: {
        id: provider.id,
        trustedRemoteSessionIssuerId: trusted.id,
      },
    }),
  );
}

/** Stops trusting any identity provider's sign-in for the provider. */
export function turnOffEma(
  core: GramCore,
  provider: UserSessionIssuer,
): Promise<UserSessionIssuer> {
  return unwrapAsync(
    organizationUserSessionIssuersUpdate(core, {
      updateOrganizationUserSessionIssuerForm: {
        id: provider.id,
        trustedRemoteSessionIssuerId: "",
      },
    }),
  );
}

function sameIssuer(a: string, b: string): boolean {
  return a.replace(/\/+$/, "") === b.replace(/\/+$/, "");
}
