import { createFileRoute } from "@tanstack/react-router";

import { AssistantsRoute } from "@/pages/organization/Assistants";

export const Route = createFileRoute("/organizations/$idOrSlug/assistants")({
  component: AssistantsRoute,
  staticData: { crumb: "Assistants" },
});
