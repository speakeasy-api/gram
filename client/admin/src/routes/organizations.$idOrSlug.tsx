import { createFileRoute } from "@tanstack/react-router";

import { RecordLayout } from "@/pages/organization/RecordLayout";

export const Route = createFileRoute("/organizations/$idOrSlug")({
  component: RecordLayout,
});
