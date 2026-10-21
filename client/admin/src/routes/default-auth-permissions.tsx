import { createFileRoute } from "@tanstack/react-router";
import { DefaultAuthPermissionsPage } from "@/pages/DefaultAuthPermissions";

export const Route = createFileRoute("/default-auth-permissions")({
  component: DefaultAuthPermissionsPage,
  staticData: { crumb: "Default auth permissions" },
});
