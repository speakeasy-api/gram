import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { PluginRepositoryMigrationsRoute } from "@/pages/PluginRepositoryMigrations";

export const Route = createFileRoute("/plugin-repository-migrations")({
  validateSearch: z.object({
    after: z.uuid().optional().catch(undefined),
    project: z.uuid().optional().catch(undefined),
  }),
  component: PluginRepositoryMigrationsRoute,
  staticData: { crumb: "Plugin repository migrations" },
});
