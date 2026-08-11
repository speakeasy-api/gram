import { createFileRoute } from "@tanstack/react-router";

import { OrganizationDetail } from "@/pages/OrganizationDetail";

export const Route = createFileRoute("/organizations/$idOrSlug")({
  component: OrganizationDetail,
});
