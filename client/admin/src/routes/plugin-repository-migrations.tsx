import { createFileRoute } from "@tanstack/react-router";
import { PluginRepositoryMigrationsPage } from "@/pages/PluginRepositoryMigrations";

export const Route = createFileRoute("/plugin-repository-migrations")({
  component: PluginRepositoryMigrationsPage,
  staticData: { crumb: "Plugin repository migrations" },
});
