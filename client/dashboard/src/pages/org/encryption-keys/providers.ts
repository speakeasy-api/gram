import type { CreateGcpKmsKeyFormAlgorithm } from "@gram/client/models/components/creategcpkmskeyform.js";

// The KMS providers an organization admin can create a key for today. The
// organization API also supports AWS KMS, but a key is only as reachable as the
// credential behind it, and the credentials page is GCP-only because Gram has no
// AWS identity to assume a customer role from. An AWS key would therefore have
// no credential to pick, no detail page to link to, and no way to verify — so
// the selector stays GCP-only until that changes.
export const KMS_KEY_PROVIDERS = [
  { value: "gcp_kms", label: "Google Cloud KMS" },
] as const;

export type KmsKeyProvider = (typeof KMS_KEY_PROVIDERS)[number]["value"];

// The signing algorithms an external key can record. Widening this is an
// interoperability decision rather than a mechanical one, and the server rejects
// anything outside the set, so the two lists have to stay aligned.
export type KeyAlgorithm = CreateGcpKmsKeyFormAlgorithm;

export const KEY_ALGORITHMS: { value: KeyAlgorithm; label: string }[] = [
  { value: "RS256", label: "RS256 (RSA, SHA-256)" },
  { value: "ES256", label: "ES256 (ECDSA P-256, SHA-256)" },
];

// RS256 is the default because it is the algorithm every JWT verifier
// implements. The choice still has to match what the key actually signs with,
// which is why verify reports a mismatch rather than accepting one.
export const DEFAULT_KEY_ALGORITHM: KeyAlgorithm = "RS256";

// PROVIDER_SLUGS maps the API's `provider` discriminator to the URL segment used
// by the detail route.
const PROVIDER_SLUGS: Record<string, string> = {
  gcp_kms: "gcp",
  aws_kms: "aws",
};

const PROVIDERS_BY_SLUG: Record<string, string> = Object.fromEntries(
  Object.entries(PROVIDER_SLUGS).map(([provider, slug]) => [slug, provider]),
);

// providerSlug converts a `provider` discriminator to its URL segment.
export function providerSlug(provider: string): string {
  return PROVIDER_SLUGS[provider] ?? provider;
}

// providerFromSlug converts a URL segment back to a `provider` discriminator,
// returning undefined for a segment that names no known provider so callers can
// treat a hand-edited URL as not-found rather than querying for nonsense.
export function providerFromSlug(slug: string): string | undefined {
  return PROVIDERS_BY_SLUG[slug];
}

// providerLabel maps a supertype `provider` discriminator to a display name.
export function providerLabel(provider: string): string {
  switch (provider) {
    case "gcp_kms":
      return "Google Cloud KMS";
    case "aws_kms":
      return "AWS KMS";
    default:
      return provider;
  }
}
