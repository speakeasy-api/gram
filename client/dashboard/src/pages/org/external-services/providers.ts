// The external-service providers an organization admin can create today. The
// organization API also supports AWS, but Gram has no AWS identity to assume a
// customer role from yet, so an AWS credential could be stored and never
// verified. Until that exists the selector stays GCP-only.
export const EXTERNAL_SERVICE_PROVIDERS = [
  { value: "gcp_iam", label: "Google Cloud Platform" },
] as const;

export type ExternalServiceProvider =
  (typeof EXTERNAL_SERVICE_PROVIDERS)[number]["value"];

// PROVIDER_SLUGS maps the API's `provider` discriminator to the URL segment used
// by the detail route. The detail page is per-provider (each provider has its own
// get/update endpoints and its own fields), so the provider has to be recoverable
// from the URL alone rather than carried over from the list.
const PROVIDER_SLUGS: Record<string, string> = {
  gcp_iam: "gcp",
  aws_iam: "aws",
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
    case "gcp_iam":
      return "Google Cloud Platform";
    case "aws_iam":
      return "Amazon Web Services";
    default:
      return provider;
  }
}
