import { createFileRoute } from "@tanstack/react-router";

import { CustomerUsage } from "@/pages/customer-usage/CustomerUsage";
import { customerUsageSearch } from "@/pages/customer-usage/customerUsageSearch";

export const Route = createFileRoute("/customer-usage")({
  component: CustomerUsage,
  validateSearch: customerUsageSearch,
  staticData: { crumb: "Customer usage" },
});
