import { createFileRoute } from "@tanstack/react-router";

import { HooksRollout } from "@/pages/hooks-rollout/HooksRollout";

export const Route = createFileRoute("/hooks-rollout")({
  component: HooksRollout,
  staticData: { crumb: "Hooks rollout" },
});
