import { createFileRoute } from "@tanstack/react-router";
import { RegistryEntryPage } from "@/pages/registry/RegistryEntryPage";
import { registryEntryNameKey } from "@/lib/registryEntryName";
export const Route = createFileRoute("/registry/$entryId")({
  component: RegistryEntryPage,
  staticData: {
    // The name the entry read records as it lands; the entry itself holds
    // only its raw JSON.
    crumb: ({ entryId }) =>
      entryId ? { queryKey: registryEntryNameKey(entryId) } : undefined,
  },
});
