import { createFileRoute } from "@tanstack/react-router";
import { IssuerConvergence } from "@/pages/remote-session-issuers/IssuerDetail";
export const Route = createFileRoute(
  "/remote-session-issuers/$issuerId/convergence",
)({ component: IssuerConvergence, staticData: { crumb: "Convergence" } });
