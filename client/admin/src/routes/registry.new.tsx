import { createFileRoute } from "@tanstack/react-router";
import { NewRegistryEntryPage } from "@/pages/registry/RegistryEntryPage";
export const Route = createFileRoute("/registry/new")({
  component: NewRegistryEntryPage,
  staticData: { crumb: "New entry" },
});
