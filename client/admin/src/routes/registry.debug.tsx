import { createFileRoute } from "@tanstack/react-router";
import { RegistryDebugPage } from "@/pages/registry/debugger/RegistryDebugPage";
import { registryDebugSearch } from "@/pages/registry/debugger/debugSearch";

// A static segment, so it ranks above `$entryId`.
export const Route = createFileRoute("/registry/debug")({
  component: RegistryDebugPage,
  validateSearch: registryDebugSearch,
  staticData: { crumb: "Debugger" },
});
