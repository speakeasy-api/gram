import { createFileRoute } from "@tanstack/react-router";
import { IssuerOverview } from "@/pages/remote-session-issuers/IssuerDetail";
export const Route = createFileRoute("/remote-session-issuers/$issuerId/")({
  component: IssuerOverview,
  staticData: { crumb: "Overview" },
});
