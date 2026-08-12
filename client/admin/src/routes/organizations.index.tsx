import { createFileRoute } from "@tanstack/react-router";

import { OrganizationsList } from "@/pages/OrganizationsList";

export const Route = createFileRoute("/organizations/")({
  component: OrganizationsList,
});
