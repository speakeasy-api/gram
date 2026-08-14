import { useSlugs } from "@/contexts/Sdk";
import { useLocation } from "react-router";

/**
 * Whether the org-home welcome banner is on screen. The header's "Finish
 * setup" banner reads it too, so the two never show at once. Route-based for
 * now; the real zero-data rule lands here.
 */
export function useOrgWelcomeBanner(): { visible: boolean } {
  const { orgSlug, projectSlug } = useSlugs();
  const { pathname } = useLocation();

  const onOrgHome =
    Boolean(orgSlug) &&
    !projectSlug &&
    pathname.replace(/\/+$/, "") === `/${orgSlug}`;

  return { visible: onOrgHome };
}
