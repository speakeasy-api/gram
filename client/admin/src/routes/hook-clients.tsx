import { createFileRoute } from "@tanstack/react-router";

import { HookClients } from "@/pages/hook-clients/HookClients";

export const Route = createFileRoute("/hook-clients")({
  component: HookClients,
  staticData: { crumb: "Hook clients" },
});
