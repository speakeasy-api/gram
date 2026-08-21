import { createFileRoute } from "@tanstack/react-router";

import { BillingRoute } from "@/pages/organization/Billing";

export const Route = createFileRoute("/organizations/$idOrSlug/billing")({
  component: BillingRoute,
  staticData: { crumb: "Billing" },
});
