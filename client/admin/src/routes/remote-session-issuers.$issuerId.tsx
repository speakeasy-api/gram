import { createFileRoute } from "@tanstack/react-router";
import { IssuerDetail } from "@/pages/remote-session-issuers/IssuerDetail";
import { adminGetGlobalIssuerQuery } from "@/lib/gramAdminClient";
export const Route = createFileRoute("/remote-session-issuers/$issuerId")({
  component: IssuerDetail,
  staticData: {
    crumb: (params) =>
      params.issuerId
        ? {
            queryKey: adminGetGlobalIssuerQuery({ id: params.issuerId })
              .queryKey,
          }
        : undefined,
  },
});
