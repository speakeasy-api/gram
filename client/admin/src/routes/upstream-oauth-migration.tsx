import { createFileRoute } from "@tanstack/react-router";
import { UpstreamOAuthMigrationPage } from "@/pages/UpstreamOAuthMigration";

export const Route = createFileRoute("/upstream-oauth-migration")({
  component: UpstreamOAuthMigrationPage,
  staticData: { crumb: "Upstream OAuth migration" },
});
