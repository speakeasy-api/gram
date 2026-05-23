import { Link } from "react-router";

import { useOrganization } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";

import { GramLogo } from "./gram-logo";

/**
 * Top-of-sidebar brand strip: Gram logo, then a hairline divider with the
 * current org slug below. Mounts inside <SidebarHeader>. When the sidebar
 * collapses to icons we hide the wordmark.
 */
export function SidebarBrand() {
  const organization = useOrganization();
  const { hasAnyScope } = useRBAC();
  const canAccessOrgRoutes = hasAnyScope(["org:read", "org:admin"]);

  return (
    <div className="flex flex-col gap-2 px-2 pt-2 pb-1">
      <Link
        to={canAccessOrgRoutes ? `/${organization.slug}` : "#"}
        className="flex items-center gap-2 hover:no-underline"
      >
        <GramLogo className="w-24 group-data-[collapsible=icon]:w-6" />
      </Link>
      <div className="text-muted-foreground truncate px-1 text-xs font-medium group-data-[collapsible=icon]:hidden">
        {organization.slug}
      </div>
    </div>
  );
}
