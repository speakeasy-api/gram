import { createFileRoute } from "@tanstack/react-router";
export const Route = createFileRoute("/remote-session-issuers")({
  staticData: { crumb: "Remote session issuers" },
});
