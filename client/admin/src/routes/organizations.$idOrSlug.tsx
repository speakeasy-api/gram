import { createFileRoute } from "@tanstack/react-router";

import { organizationQuery } from "@/lib/adminQueries";
import { RecordLayout } from "@/pages/organization/RecordLayout";

export const Route = createFileRoute("/organizations/$idOrSlug")({
  component: RecordLayout,
  staticData: {
    crumb: ({ idOrSlug }) =>
      idOrSlug ? organizationQuery(idOrSlug) : undefined,
  },
});
