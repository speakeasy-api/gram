import { createFileRoute } from "@tanstack/react-router";
import { OrganizationsList } from "@/pages/organizations/index";
import { organizationsSearchSchema } from "@/lib/organizationSearch";

export const Route = createFileRoute("/organizations/")({
  component: OrganizationsList,
  validateSearch: organizationsSearchSchema,
});
