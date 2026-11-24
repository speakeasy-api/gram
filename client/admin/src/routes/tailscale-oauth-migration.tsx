import { createFileRoute } from "@tanstack/react-router";
import { TailscaleOAuthMigrationPage } from "@/pages/TailscaleOAuthMigration";

export const Route = createFileRoute("/tailscale-oauth-migration")({
  component: TailscaleOAuthMigrationPage,
  staticData: { crumb: "Tailscale OAuth migration" },
});
