import { createFileRoute } from "@tanstack/react-router";

import { WorkloadIdentityRoute } from "@/pages/organization/WorkloadIdentity";

export const Route = createFileRoute(
  "/organizations/$idOrSlug/workload-identity",
)({
  component: WorkloadIdentityRoute,
  staticData: { crumb: "Workload identity" },
});
