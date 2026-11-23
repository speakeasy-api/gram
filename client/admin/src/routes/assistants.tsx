import { createFileRoute } from "@tanstack/react-router";
import { AssistantsPage } from "@/pages/Assistants";

export const Route = createFileRoute("/assistants")({
  component: AssistantsPage,
  staticData: { crumb: "Assistants" },
  validateSearch: (
    search: Record<string, unknown>,
  ): { organization?: string } => ({
    organization:
      typeof search.organization === "string" ? search.organization : undefined,
  }),
});
