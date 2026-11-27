import { createFileRoute } from "@tanstack/react-router";
import { SecretBackfillsPage } from "@/pages/SecretBackfills";

export const Route = createFileRoute("/secret-backfills")({
  component: SecretBackfillsPage,
  staticData: { crumb: "Secret backfills" },
});
