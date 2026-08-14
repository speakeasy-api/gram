import { useSlugs } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useLocation } from "react-router";

/**
 * Whether the org-home welcome banner is on screen. The header's "Finish
 * setup" banner reads it too, so the two never show at once. Route-based for
 * now; the real zero-data rule lands here.
 *
 * Admins only: every first move it offers is an admin task. Non-admins get
 * their own card later.
 */
export function useOrgWelcomeBanner(): { visible: boolean } {
  const { orgSlug, projectSlug } = useSlugs();
  const { pathname } = useLocation();
  const { hasScope } = useRBAC();

  const onOrgHome =
    Boolean(orgSlug) &&
    !projectSlug &&
    pathname.replace(/\/+$/, "") === `/${orgSlug}`;

  return { visible: onOrgHome && hasScope("org:admin") };
}
