import { createFileRoute } from "@tanstack/react-router";
import { RegistryList } from "@/pages/registry/RegistryList";
export const Route = createFileRoute("/registry")({
  component: RegistryList,
  staticData: { crumb: "Registry" },
});
