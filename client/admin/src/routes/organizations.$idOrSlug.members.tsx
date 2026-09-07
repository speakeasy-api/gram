import { createFileRoute } from "@tanstack/react-router";

import { MembersRoute } from "@/pages/organization/Members";

export const Route = createFileRoute("/organizations/$idOrSlug/members")({
  component: MembersRoute,
  staticData: { crumb: "Members" },
});
