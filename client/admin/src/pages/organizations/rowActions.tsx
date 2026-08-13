import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useCallback } from "react";

import { organizationQuery } from "@/lib/adminQueries";
import type { AdminOrganization } from "@/lib/gramAdminApi";

// The row's own behavior lives here, so the slices that add a row menu, a
// disable action and a trial extension all land in one file.
export function useOpenOrganization(): (org: AdminOrganization) => void {
  const navigate = useNavigate();
  const qc = useQueryClient();

  return useCallback(
    (org: AdminOrganization) => {
      const idOrSlug = org.slug || org.id;
      // The row already holds the whole record, so the detail page paints and
      // starts its own queries without a round trip to organization.get.
      qc.setQueryData(organizationQuery(idOrSlug).queryKey, org);
      void navigate({ to: "/organizations/$idOrSlug", params: { idOrSlug } });
    },
    [navigate, qc],
  );
}
