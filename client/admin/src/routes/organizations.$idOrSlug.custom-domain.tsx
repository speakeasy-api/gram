import { createFileRoute } from "@tanstack/react-router";

import { CustomDomainRoute } from "@/pages/organization/CustomDomain";

export const Route = createFileRoute("/organizations/$idOrSlug/custom-domain")({
  component: CustomDomainRoute,
  staticData: { crumb: "Custom domain" },
});
