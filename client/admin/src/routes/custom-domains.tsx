import { createFileRoute } from "@tanstack/react-router";

import { CustomDomains } from "@/pages/custom-domains/CustomDomains";
import { customDomainsSearch } from "@/pages/custom-domains/customDomainsSearch";

export const Route = createFileRoute("/custom-domains")({
  component: CustomDomains,
  validateSearch: customDomainsSearch,
  staticData: { crumb: "Custom domains" },
});
