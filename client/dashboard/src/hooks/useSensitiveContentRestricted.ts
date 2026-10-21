import { useSession } from "@/contexts/Auth";
import { DEMO_ORG_SLUG } from "@/lib/demo";

/** Mirrors the server's impersonation privacy gate; API authorization stays authoritative. */
export function useSensitiveContentRestricted(): boolean {
  const { organization, organizationOverride, impersonatorEmail } =
    useSession();
  return (
    organization.slug !== DEMO_ORG_SLUG &&
    (organizationOverride || Boolean(impersonatorEmail))
  );
}
