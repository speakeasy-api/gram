import type { CustomDomain } from "@gram/client/models/components/customdomain.js";

type RecommendedUpdateDomain = Pick<
  CustomDomain,
  "verified" | "activated" | "dnsTarget" | "dnsRoutingMethod"
>;

/**
 * Whether a working domain should move from the shared CNAME to its designated
 * target. Domains observed as A records are left alone: a flattened CNAME,
 * ALIAS or ANAME record (at an apex, or a subdomain behind a provider that
 * flattens CNAMEs) resolves to addresses too, so a domain already pointing at
 * its designated target would see the recommendation forever. Health checks,
 * which clear the recommendation, only run on activated domains.
 */
export function showRecommendedDnsUpdate(
  domain: RecommendedUpdateDomain,
): boolean {
  return (
    domain.verified &&
    domain.activated &&
    Boolean(domain.dnsTarget) &&
    domain.dnsRoutingMethod === "shared_cname"
  );
}
