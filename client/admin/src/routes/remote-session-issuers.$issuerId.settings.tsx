import { createFileRoute } from "@tanstack/react-router";
import { IssuerSettings } from "@/pages/remote-session-issuers/IssuerDetail";
export const Route = createFileRoute(
  "/remote-session-issuers/$issuerId/settings",
)({ component: IssuerSettings, staticData: { crumb: "Settings" } });
