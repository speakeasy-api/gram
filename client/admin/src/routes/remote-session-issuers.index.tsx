import { createFileRoute } from "@tanstack/react-router";
import { IssuerList } from "@/pages/remote-session-issuers/IssuerList";
export const Route = createFileRoute("/remote-session-issuers/")({
  component: IssuerList,
  staticData: { crumb: "" },
});
