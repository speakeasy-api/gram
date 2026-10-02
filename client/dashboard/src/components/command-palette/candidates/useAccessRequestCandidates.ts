import { useSlugs } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useListMcpApprovalRequests } from "@gram/client/react-query/listMcpApprovalRequests.js";
import { useMemo } from "react";
import { useNavigate } from "react-router";
import type { LauncherCandidate } from "./types";

/** Pending shadow-MCP access requests. Admin-only; the caller gates `enabled`. */
export function useAccessRequestCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const routes = useRoutes();
  const navigate = useNavigate();
  const { projectSlug = "" } = useSlugs();
  const { data } = useListMcpApprovalRequests(
    { status: "requested", gramProject: projectSlug },
    undefined,
    // Never throws: a failed list degrades to no candidates.
    { enabled, throwOnError: false },
  );

  return useMemo(
    () =>
      (data?.requests ?? []).map((request): LauncherCandidate => ({
        id: `access_request:${request.id}`,
        kind: "access_request",
        title: request.targetRaw,
        detail: `Access request · ${request.status}`,
        keywords: ["access", "request", request.id],
        verbs: ["open"],
        icon: "inbox",
        group: "Access Requests",
        // stdio targets have no server page; their row on the servers table
        // opens the review sheet.
        run: () => {
          void navigate(
            request.serverSlug
              ? routes.shadowMCP.detail.href(request.serverSlug)
              : routes.shadowMCP.href(),
          );
        },
      })),
    [data, routes, navigate],
  );
}
