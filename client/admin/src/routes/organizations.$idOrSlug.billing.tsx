import { createFileRoute } from "@tanstack/react-router";

import { BillingRoute } from "@/pages/organization/Billing";
import { billingUsageSearch } from "@/pages/organization/billingUsageSearch";

export const Route = createFileRoute("/organizations/$idOrSlug/billing")({
  component: BillingRoute,
  validateSearch: billingUsageSearch,
  staticData: { crumb: "Billing" },
});
